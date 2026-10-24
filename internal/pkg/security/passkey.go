package security

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"net/url"
	"os"
)

const (
	flagUserPresent  = 0x01 // Bit 0 : User Present (UP)
	flagUserVerified = 0x04 // Bit 2 : User Verified (UV - FaceID / TouchID / Biométrie)
)

type webAuthnClientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

func GenerateWebAuthnChallenge() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func GetRPID() string {
	if rp := os.Getenv("NUMAN_RP_ID"); rp != "" {
		return rp
	}
	return "localhost"
}

// EncodePublicKeyStorage normalise une clé publique P-256 (64 ou 65 octets) ou Ed25519 (32 octets).
func EncodePublicKeyStorage(rawKey []byte) ([]byte, error) {
	if len(rawKey) == ed25519.PublicKeySize {
		return append([]byte{2}, rawKey...), nil
	}
	if len(rawKey) == 65 && rawKey[0] == 0x04 {
		return append([]byte{1}, rawKey[1:]...), nil
	}
	if len(rawKey) == 64 {
		return append([]byte{1}, rawKey...), nil
	}
	return nil, errors.New("format de clé publique Passkey non supporté (attendu P-256 ou Ed25519)")
}

func VerifyPasskeyRegistration(expectedChallenge, clientDataB64, authDataB64, pubKeyB64 string) ([]byte, int64, error) {
	authData, err := verifyCommonWebAuthn("webauthn.create", expectedChallenge, clientDataB64, authDataB64)
	if err != nil {
		return nil, 0, err
	}

	rawPubKey, err := base64.RawURLEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return nil, 0, errors.New("clé publique Passkey Base64URL invalide")
	}

	encodedKey, err := EncodePublicKeyStorage(rawPubKey)
	if err != nil {
		return nil, 0, err
	}

	signCount := int64(binary.BigEndian.Uint32(authData[33:37]))
	return encodedKey, signCount, nil
}

func VerifyPasskeyAssertion(
	expectedChallenge string,
	storedPublicKey []byte,
	storedSignCount int64,
	clientDataB64 string,
	authDataB64 string,
	signatureB64 string,
) (newSignCount int64, isCloned bool, err error) {
	authData, err := verifyCommonWebAuthn("webauthn.get", expectedChallenge, clientDataB64, authDataB64)
	if err != nil {
		return 0, false, err
	}

	clientDataRaw, err := base64.RawURLEncoding.DecodeString(clientDataB64)
	if err != nil {
		return 0, false, errors.New("signature Base64URL invalide")
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(signatureB64)
	if err != nil {
		return 0, false, errors.New("signature Base64URL invalide")
	}

	clientDataHash := sha256.Sum256(clientDataRaw)
	signedPayload := append(append([]byte{}, authData...), clientDataHash[:]...)

	if len(storedPublicKey) < 2 {
		return 0, false, errors.New("clé publique stockée invalide")
	}

	switch storedPublicKey[0] {
	case 1: // ES256 (ECDSA P-256 + SHA-256)
		keyMat := storedPublicKey[1:]
		if len(keyMat) != 64 {
			return 0, false, errors.New("clé P-256 corrompue")
		}
		pub := &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(keyMat[:32]),
			Y:     new(big.Int).SetBytes(keyMat[32:]),
		}
		digest := sha256.Sum256(signedPayload)
		if !ecdsa.VerifyASN1(pub, digest[:], sigBytes) {
			return 0, false, errors.New("signature ECDSA P-256 invalide")
		}
	case 2: // EdDSA (Ed25519)
		keyMat := storedPublicKey[1:]
		if len(keyMat) != ed25519.PublicKeySize {
			return 0, false, errors.New("clé Ed25519 corrompue")
		}
		if !ed25519.Verify(ed25519.PublicKey(keyMat), signedPayload, sigBytes) {
			return 0, false, errors.New("signature Ed25519 invalide")
		}
	default:
		return 0, false, errors.New("algorithme cryptographique Passkey inconnu")
	}

	incomingCount := int64(binary.BigEndian.Uint32(authData[33:37]))
	if (storedSignCount > 0 || incomingCount > 0) && incomingCount <= storedSignCount {
		return incomingCount, true, errors.New("compteur de signature Passkey invalide (clonage matériel détecté)")
	}

	return incomingCount, false, nil
}

func verifyCommonWebAuthn(expectedType, expectedChallenge, clientDataB64, authDataB64 string) ([]byte, error) {
	clientDataRaw, err := base64.RawURLEncoding.DecodeString(clientDataB64)
	if err != nil {
		return nil, errors.New("clientDataJSON Base64URL invalide")
	}

	var cd webAuthnClientData
	if err := json.Unmarshal(clientDataRaw, &cd); err != nil {
		return nil, errors.New("clientDataJSON malformé")
	}
	if cd.Type != expectedType {
		return nil, errors.New("type d'opération WebAuthn inattendu")
	}
	if cd.Challenge != expectedChallenge {
		return nil, errors.New("challenge WebAuthn invalide")
	}

	rpID := GetRPID()
	if rpID != "localhost" {
		parsedOrigin, errOrigin := url.Parse(cd.Origin)
		if errOrigin != nil || parsedOrigin.Hostname() != rpID {
			return nil, errors.New("origine WebAuthn non autorisée")
		}
	}

	authData, err := base64.RawURLEncoding.DecodeString(authDataB64)
	if err != nil || len(authData) < 37 {
		return nil, errors.New("authenticatorData invalide ou tronqué")
	}

	if rpID != "localhost" {
		expectedRPHash := sha256.Sum256([]byte(rpID))
		if !bytes.Equal(authData[:32], expectedRPHash[:]) {
			return nil, errors.New("RP ID Hash invalide")
		}
	}

	flags := authData[32]
	if (flags&flagUserPresent) == 0 || (flags&flagUserVerified) == 0 {
		return nil, errors.New("vérification biométrique ou matérielle absente (flags UP/UV requis)")
	}

	return authData, nil
}
