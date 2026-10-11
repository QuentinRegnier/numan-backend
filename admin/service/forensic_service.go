package service

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

	"github.com/gen2brain/avif" // AVIF (Format natif de numan)
)

// WatermarkReport représente les données extraites d'une image suspecte
type WatermarkReport struct {
	OriginalAuthorID string
	PostID           string
	CulpritID        string
	LeakTimestamp    time.Time
}

// ExtractForensicData analyse une image pour retrouver le tatouage DCT.
func ExtractForensicData(imageBuffer []byte) (*WatermarkReport, error) {
	// 1. Décodage de l'image binaire vers une matrice de pixels (RAM)
	// On tente l'AVIF en premier (notre format de base).
	img, err := avif.Decode(bytes.NewReader(imageBuffer))
	if err != nil {
		// MAGIE DU GO : Si ce n'est pas de l'AVIF, image.Decode va automatiquement
		// tester JPEG, PNG, WEBP, TIFF et HEIC grâce aux imports '_' ci-dessus.
		img, _, err = image.Decode(bytes.NewReader(imageBuffer))
		if err != nil {
			return nil, numan_error.NewBadRequest("IMAGE_DECODE_FAILED", "Impossible de décoder l'image fuitée (formats supportés: AVIF, HEIC, WEBP, JPEG, PNG, TIFF).", err)
		}
	}

	// 2. Extraction des bits cachés via DCT
	extractedBits := extractBitsFromDCT(img)

	// 3. Reconstitution de la chaîne (8 bits -> 1 caractère)
	payload := bitsToString(extractedBits)

	// 4. Parsing des données extraites
	return parseWatermark(payload)
}

// extractBitsFromDCT découpe l'image et lit le signal mathématique
func extractBitsFromDCT(img image.Image) []int {
	bounds := img.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y

	var bits []int

	// On découpe l'image en blocs de 8x8 pixels
	for y := 0; y < height-8; y += 8 {
		for x := 0; x < width-8; x += 8 {

			// Pour chaque bloc, on lit 1 bit de notre charge utile
			bit := readBitFromKochZhaoBlock(img, x, y)
			bits = append(bits, bit)

			// Sécurité de performance : On s'arrête à 256 bits (soit 32 caractères ASCII).
			// C'est suffisant pour lire "A:42|P:8899|R:105|T:1715152800"
			if len(bits) >= 256 {
				return bits
			}
		}
	}
	return bits
}

// readBitFromKochZhaoBlock calcule la DCT 2D ciblée et applique la règle de décision
func readBitFromKochZhaoBlock(img image.Image, startX, startY int) int {
	var f [8][8]float64

	// 1. Extraction de la luminance (Matrice Y en niveaux de gris)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			r, g, b, _ := img.At(startX+x, startY+y).RGBA()
			// Formule standard de conversion colorimétrique (Rec. 601)
			lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
			f[x][y] = lum / 257.0 // Normalisation de 0 à 255
		}
	}

	// 2. Calcul des fréquences moyennes : milieu de la bande
	// On esquive les basses fréquences (altèrent l'image) et hautes fréquences (détruites par l'appareil photo)
	coeff1 := computeDCTCoeff(f, 4, 5)
	coeff2 := computeDCTCoeff(f, 5, 4)

	// 3. Règle de décision différentielle (Robuste au trou analogique)
	if coeff1 > coeff2 {
		return 1
	}
	return 0
}

// computeDCTCoeff applique la formule mathématique de la DCT-II
func computeDCTCoeff(f [8][8]float64, u, v int) float64 {
	sum := 0.0
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			cosX := math.Cos((float64(2*x+1) * float64(u) * math.Pi) / 16.0)
			cosY := math.Cos((float64(2*y+1) * float64(v) * math.Pi) / 16.0)
			sum += f[x][y] * cosX * cosY
		}
	}

	cu, cv := 1.0, 1.0
	if u == 0 {
		cu = 1.0 / math.Sqrt(2)
	}
	if v == 0 {
		cv = 1.0 / math.Sqrt(2)
	}

	return 0.25 * cu * cv * sum
}

// bitsToString convertit le tableau de bits bruts en texte lisible
func bitsToString(bits []int) string {
	var sb strings.Builder

	// On itère de 8 en 8 (1 octet = 8 bits)
	for i := 0; i < len(bits)-7; i += 8 {
		var charCode byte = 0
		for j := 0; j < 8; j++ {
			charCode = (charCode << 1) | byte(bits[i+j])
		}
		if charCode == 0 {
			break // Fin de la chaîne (caractère nul atteint)
		}
		sb.WriteByte(charCode)
	}

	// Nettoyage basique en cas de bruit optique (on ignore les caractères non imprimables)
	return strings.Map(func(r rune) rune {
		if r >= 32 && r <= 126 {
			return r
		}
		return -1
	}, sb.String())
}

// parseWatermark décode la chaîne textuelle brute vers l'objet structuré
func parseWatermark(payload string) (*WatermarkReport, error) {
	parts := strings.Split(payload, "|")
	if len(parts) != 4 {
		return nil, numan_error.NewBadRequest("INVALID_WATERMARK_FORMAT", "Format de tatouage corrompu ou inexistant.", nil)
	}

	report := &WatermarkReport{}
	for _, p := range parts {
		kv := strings.Split(p, ":")
		if len(kv) != 2 {
			continue
		}

		switch kv[0] {
		case "A":
			report.OriginalAuthorID = kv[1]
		case "P":
			report.PostID = kv[1]
		case "R":
			report.CulpritID = kv[1]
		case "T":
			var sec int64
			if _, sscanf := fmt.Sscanf(kv[1], "%d", &sec); sscanf != nil {
				return nil, sscanf
			}
			report.LeakTimestamp = time.Unix(sec, 0)
		}
	}
	return report, nil
}
