package media_service_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraMinio "github.com/QuentinRegnier/numan-backend/internal/infrastructure/minio"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gen2brain/avif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func buildTestPNG(t *testing.T, w, h int) *bytes.Reader {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 120, G: 180, B: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return bytes.NewReader(buf.Bytes())
}

func buildTestJPEG(t *testing.T, w, h int) *bytes.Reader {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}))
	return bytes.NewReader(buf.Bytes())
}

func buildTestAnimatedGIF(t *testing.T) ([]byte, *bytes.Reader) {
	t.Helper()
	palette := []color.Color{color.Black, color.White}
	f1 := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)
	f2 := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)
	f2.SetColorIndex(1, 1, 1)

	g := &gif.GIF{
		Image: []*image.Paletted{f1, f2},
		Delay: []int{10, 10},
	}
	var buf bytes.Buffer
	require.NoError(t, gif.EncodeAll(&buf, g))
	raw := buf.Bytes()
	return raw, bytes.NewReader(raw)
}

func buildTestAnimatedWebP(w, h int) ([]byte, *bytes.Reader) {
	// Valid RIFF/WEBP with VP8X (animation flag 0x02) + ANIM chunk
	vp8x := make([]byte, 10)
	vp8x[0] = 0x02 // animation flag
	wMinus1 := w - 1
	hMinus1 := h - 1
	vp8x[4] = byte(wMinus1 & 0xFF)
	vp8x[5] = byte((wMinus1 >> 8) & 0xFF)
	vp8x[6] = byte((wMinus1 >> 16) & 0xFF)
	vp8x[7] = byte(hMinus1 & 0xFF)
	vp8x[8] = byte((hMinus1 >> 8) & 0xFF)
	vp8x[9] = byte((hMinus1 >> 16) & 0xFF)

	anim := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00} // BG color + loop count

	var chunks bytes.Buffer
	chunks.WriteString("WEBP")
	chunks.WriteString("VP8X")
	_ = binary.Write(&chunks, binary.LittleEndian, uint32(len(vp8x)))
	chunks.Write(vp8x)
	chunks.WriteString("ANIM")
	_ = binary.Write(&chunks, binary.LittleEndian, uint32(len(anim)))
	chunks.Write(anim)

	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(chunks.Len()))
	out.Write(chunks.Bytes())

	raw := out.Bytes()
	return raw, bytes.NewReader(raw)
}

func TestGeneratePublicMediaURL(t *testing.T) {
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public/")

	url := media_service.GeneratePublicMediaURL("articles/123e4567.avif")
	assert.Equal(t, "https://cdn.numan.app/public/articles/123e4567.avif", url)
	assert.NotContains(t, url, "sig=")
	assert.NotContains(t, url, "/process")

	// Leading slash trimmed
	assert.Equal(t, "https://cdn.numan.app/public/articles/doc.md", media_service.GeneratePublicMediaURL("/articles/doc.md"))

	// Rejects external URLs, traversal, empty
	assert.Empty(t, media_service.GeneratePublicMediaURL(""))
	assert.Empty(t, media_service.GeneratePublicMediaURL("https://evil.com/x.md"))
	assert.Empty(t, media_service.GeneratePublicMediaURL("//evil.com/x.md"))
	assert.Empty(t, media_service.GeneratePublicMediaURL("../secret.txt"))
	assert.Empty(t, media_service.GeneratePublicMediaURL("articles/../secret.txt"))
}

func TestUploadAdminArticleAsset_AllFormatsAndQualityTiers(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	fakeMinio, minioClient := test.SetupTestMinio(t)
	infraMinio.MinioClient = minioClient

	t.Setenv("MINIO_BUCKET_NAME", "numan-bucket")
	t.Setenv("MINIO_PUBLIC_BUCKET_NAME", "numan-public")
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")

	ctx := context.Background()
	adminID := int64(9001)

	// 1. Static PNG smaller than maxDim -> never upscaled, encoded to AVIF in numan-public
	outSmall, err := media_service.UploadAdminArticleAsset(
		ctx,
		buildTestPNG(t, 40, 20),
		adminID,
		1001,
		"image",
		"standard",
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1001), outSmall.MediaID)
	assert.True(t, strings.HasPrefix(outSmall.StoragePath, "articles/"))
	assert.True(t, strings.HasSuffix(outSmall.StoragePath, ".avif"))
	assert.Equal(t, "image/avif", outSmall.ContentType)
	assert.Equal(t, "https://cdn.numan.app/public/"+outSmall.StoragePath, outSmall.URL)

	// Verify stored in numan-public and NOT in numan-bucket
	storedAvif := fakeMinio.Buckets["numan-public"][outSmall.StoragePath]
	require.NotEmpty(t, storedAvif)
	assert.Empty(t, fakeMinio.Buckets["numan-bucket"][outSmall.StoragePath])

	// Verify decoded AVIF dimensions were NOT upscaled (still 40x20)
	cfgSmall, errCfg := avif.DecodeConfig(bytes.NewReader(storedAvif))
	require.NoError(t, errCfg)
	assert.Equal(t, 40, cfgSmall.Width)
	assert.Equal(t, 20, cfgSmall.Height)

	// 2. Static JPEG larger than standard (1024) -> downscaled proportionally without cropping
	outLarge, err := media_service.UploadAdminArticleAsset(
		ctx,
		buildTestJPEG(t, 1200, 600),
		adminID,
		1002,
		"image",
		"standard",
	)
	require.NoError(t, err)
	storedLarge := fakeMinio.Buckets["numan-public"][outLarge.StoragePath]
	require.NotEmpty(t, storedLarge)
	cfgLarge, errCfgL := avif.DecodeConfig(bytes.NewReader(storedLarge))
	require.NoError(t, errCfgL)
	assert.Equal(t, 1024, cfgLarge.Width)
	assert.Equal(t, 512, cfgLarge.Height)

	// 3. High tier (2048) preserves 1200x600 without downscaling
	outHigh, err := media_service.UploadAdminArticleAsset(
		ctx,
		buildTestJPEG(t, 1200, 600),
		adminID,
		1003,
		"image",
		"high",
	)
	require.NoError(t, err)
	storedHigh := fakeMinio.Buckets["numan-public"][outHigh.StoragePath]
	cfgHigh, errCfgH := avif.DecodeConfig(bytes.NewReader(storedHigh))
	require.NoError(t, errCfgH)
	assert.Equal(t, 1200, cfgHigh.Width)
	assert.Equal(t, 600, cfgHigh.Height)

	// 4. Animated GIF preserved byte-for-byte
	rawGIF, gifReader := buildTestAnimatedGIF(t)
	outGIF, err := media_service.UploadAdminArticleAsset(
		ctx,
		gifReader,
		adminID,
		1004,
		"image",
		"standard",
	)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(outGIF.StoragePath, ".gif"))
	assert.Equal(t, "image/gif", outGIF.ContentType)
	assert.Equal(t, rawGIF, fakeMinio.Buckets["numan-public"][outGIF.StoragePath])

	// 5. Animated WebP preserved byte-for-byte
	rawWebP, webpReader := buildTestAnimatedWebP(64, 64)
	outWebP, err := media_service.UploadAdminArticleAsset(
		ctx,
		webpReader,
		adminID,
		1005,
		"image",
		"standard",
	)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(outWebP.StoragePath, ".webp"))
	assert.Equal(t, "image/webp", outWebP.ContentType)
	assert.Equal(t, rawWebP, fakeMinio.Buckets["numan-public"][outWebP.StoragePath])

	// 6. Valid Markdown UTF-8 preserved byte-for-byte with custom Flutter directives
	mdContent := "# Guide Numan\n\n:::cta[Découvrir](https://partenaire.fr)\n\n![Photo](https://cdn.numan.app/public/articles/1.avif)\n"
	outMD, err := media_service.UploadAdminArticleAsset(
		ctx,
		bytes.NewReader([]byte(mdContent)),
		adminID,
		1006,
		"markdown",
		"standard",
	)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(outMD.StoragePath, ".md"))
	assert.Equal(t, "text/markdown; charset=utf-8", outMD.ContentType)
	assert.Equal(t, []byte(mdContent), fakeMinio.Buckets["numan-public"][outMD.StoragePath])

	// 7. AVIF input image resized proportionally to AVIF
	imgForAvif := image.NewRGBA(image.Rect(0, 0, 1200, 600))
	var rawAvifBuf bytes.Buffer
	require.NoError(t, avif.Encode(&rawAvifBuf, imgForAvif, avif.Options{Quality: 70, Speed: 8}))
	outAvifIn, err := media_service.UploadAdminArticleAsset(
		ctx,
		bytes.NewReader(rawAvifBuf.Bytes()),
		adminID,
		1007,
		"image",
		"standard",
	)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(outAvifIn.StoragePath, ".avif"))
	assert.Equal(t, "image/avif", outAvifIn.ContentType)
	cfgAvifIn, errCfgAvifIn := avif.DecodeConfig(bytes.NewReader(fakeMinio.Buckets["numan-public"][outAvifIn.StoragePath]))
	require.NoError(t, errCfgAvifIn)
	assert.Equal(t, 1024, cfgAvifIn.Width)
	assert.Equal(t, 512, cfgAvifIn.Height)

	// 8. Static WebP converted to AVIF
	staticWebPBytes, errB64 := base64.StdEncoding.DecodeString("UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAwA0JaQAA3AA/vuUAAA=")
	require.NoError(t, errB64)
	outStaticWebP, err := media_service.UploadAdminArticleAsset(
		ctx,
		bytes.NewReader(staticWebPBytes),
		adminID,
		1008,
		"image",
		"standard",
	)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(outStaticWebP.StoragePath, ".avif"))
	assert.Equal(t, "image/avif", outStaticWebP.ContentType)
	assert.NotEmpty(t, fakeMinio.Buckets["numan-public"][outStaticWebP.StoragePath])
}

func TestUploadAdminArticleAsset_SecurityRejections(t *testing.T) {
	fakeRedis := test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	fakeMinio, minioClient := test.SetupTestMinio(t)
	infraMinio.MinioClient = minioClient

	t.Setenv("MINIO_BUCKET_NAME", "numan-bucket")
	t.Setenv("MINIO_PUBLIC_BUCKET_NAME", "numan-public")

	ctx := context.Background()
	adminID := int64(9001)

	// Invalid UTF-8 in Markdown
	_, err := media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte{0xff, 0xfe, 0xfd}), adminID, 2001, "markdown", "standard")
	assert.Error(t, err)

	// NUL byte in Markdown
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("# Title\x00Hidden")), adminID, 2002, "markdown", "standard")
	assert.Error(t, err)

	// Disguised HTML/SVG/Script/BOM/iframe in Markdown
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("<!DOCTYPE html><html><body>XSS</body></html>")), adminID, 2003, "markdown", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("<svg onload=alert(1)></svg>")), adminID, 2004, "markdown", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("\uFEFF<svg onload=alert(1)></svg>")), adminID, 20041, "markdown", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("# Test\n<script>alert(1)</script>")), adminID, 2005, "markdown", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("<iframe src=\"https://evil.com\"></iframe>")), adminID, 20051, "markdown", "standard")
	assert.Error(t, err)

	// Binary payloads disguised as Markdown (ELF, MZ/PE, PDF)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte{0x7F, 'E', 'L', 'F', 0x02, 0x01}), adminID, 20052, "markdown", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("MZ\x90\x00\x03\x00")), adminID, 20053, "markdown", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("%PDF-1.7\n%...")), adminID, 20054, "markdown", "standard")
	assert.Error(t, err)

	// Oversized Markdown (> 256 KB)
	bigMD := bytes.Repeat([]byte("a"), int(variables.MaxArticleMarkdownBytes)+10)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader(bigMD), adminID, 2006, "markdown", "standard")
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, numan_error.CodePayloadTooLarge, appErr.Code)

	// SVG, Video (MP4 / WebM), Audio (MP3 ID3 / OGG), Binary (ELF / PDF) rejected when uploaded as image
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><circle r="10"/></svg>`)), adminID, 2010, "image", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0x00, 0x00}), adminID, 2011, "image", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x42, 0x86, 0x81}), adminID, 2012, "image", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("ID3\x03\x00\x00\x00\x00\x00\x00audio-bytes")), adminID, 2013, "image", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00")), adminID, 2014, "image", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte{0x7F, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00}), adminID, 2015, "image", "standard")
	assert.Error(t, err)
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("%PDF-1.7\n")), adminID, 2016, "image", "standard")
	assert.Error(t, err)

	// Fake GIF header with corrupted body
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("GIF89a-not-a-valid-gif-stream")), adminID, 2007, "image", "standard")
	assert.Error(t, err)

	// Fake RIFF/WEBP header with invalid chunk size
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("RIFF\xff\xff\xff\x7fWEBPNotValid")), adminID, 2008, "image", "standard")
	assert.Error(t, err)

	// Oversized GIF (> 5 MB)
	oversizedGIF := make([]byte, int(variables.MaxArticleAnimatedGIFBytes)+64)
	copy(oversizedGIF, []byte("GIF89a"))
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader(oversizedGIF), adminID, 2017, "image", "standard")
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, numan_error.CodePayloadTooLarge, appErr.Code)

	// Oversized Animated WebP (> 5 MB)
	rawAnimWebP, _ := buildTestAnimatedWebP(64, 64)
	oversizedWebP := make([]byte, int(variables.MaxArticleAnimatedWebPBytes)+64)
	copy(oversizedWebP, rawAnimWebP)
	binary.LittleEndian.PutUint32(oversizedWebP[4:8], uint32(len(oversizedWebP)-8))
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader(oversizedWebP), adminID, 2018, "image", "standard")
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, numan_error.CodePayloadTooLarge, appErr.Code)

	// Oversized Static Image (> 15 MB)
	oversizedStatic := make([]byte, int(variables.MaxArticleStaticImageBytes)+64)
	copy(oversizedStatic, []byte{0xFF, 0xD8, 0xFF})
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader(oversizedStatic), adminID, 2019, "image", "standard")
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, numan_error.CodePayloadTooLarge, appErr.Code)

	// Decompression bomb via WebP VP8X claiming 20000x20000 pixels (400M > 67M max)
	_, bombWebP := buildTestAnimatedWebP(20000, 20000)
	_, err = media_service.UploadAdminArticleAsset(ctx, bombWebP, adminID, 2020, "image", "standard")
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, numan_error.CodePayloadTooLarge, appErr.Code)

	// Decompression bomb via GIF claiming 12000x12000 pixels (144M > 67M max)
	var bombGIF bytes.Buffer
	bombGIF.WriteString("GIF89a")
	_ = binary.Write(&bombGIF, binary.LittleEndian, uint16(12000))
	_ = binary.Write(&bombGIF, binary.LittleEndian, uint16(12000))
	bombGIF.Write([]byte{0x00, 0x00, 0x00, 0x3B})
	_, err = media_service.UploadAdminArticleAsset(ctx, bytes.NewReader(bombGIF.Bytes()), adminID, 2021, "image", "standard")
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, numan_error.CodePayloadTooLarge, appErr.Code)

	// Unsupported quality tier rejected
	_, err = media_service.UploadAdminArticleAsset(ctx, buildTestPNG(t, 16, 16), adminID, 2022, "image", "4096")
	assert.Error(t, err)

	// Rollback S3 when Redis L1 Set fails (SET failure)
	fakeRedis.SetFailCmd("SET", errors.New("redis set down"))
	_, errSet := media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("# Valid MD")), adminID, 2023, "markdown", "standard")
	assert.Error(t, errSet)
	fakeRedis.SetFailCmd("SET", nil)
	assert.Empty(t, fakeMinio.Buckets["numan-public"], "S3 object must be rolled back when L1 write fails")

	// Rollback L1 and S3 when Write-Behind EnqueueDB fails (RPUSH failure)
	fakeRedis.SetFailCmd("RPUSH", errors.New("redis queue down"))
	_, errRPush := media_service.UploadAdminArticleAsset(ctx, bytes.NewReader([]byte("# Valid MD 2")), adminID, 2024, "markdown", "standard")
	assert.Error(t, errRPush)
	fakeRedis.SetFailCmd("RPUSH", nil)
	assert.Empty(t, fakeMinio.Buckets["numan-public"], "S3 object must be rolled back when EnqueueDB fails")
	_, errCached := object_cache_service.GetMediaFromObjectCache(ctx, 2024)
	assert.Error(t, errCached, "L1 media cache must be rolled back when EnqueueDB fails")
}

func TestValidateAndActivateAdminArticleMedia_CrossAdminAndSemanticChecks(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()
	now := domain.NowMillis()

	adminA := int64(9001)
	adminB := int64(9002)
	regularUser := int64(1001)

	// Assets uploaded by adminA and adminB
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
		ID:          5001,
		OwnerID:     adminA,
		StoragePath: "articles/cover.avif",
		Visibility:  variables.MediaVisibilityPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}))
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
		ID:          5002,
		OwnerID:     adminB,
		StoragePath: "articles/content.md",
		Visibility:  variables.MediaVisibilityPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}))
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
		ID:          5003,
		OwnerID:     adminA,
		StoragePath: "articles/anim.gif",
		Visibility:  variables.MediaVisibilityPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}))

	// Private user media
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
		ID:          5999,
		OwnerID:     regularUser,
		StoragePath: "users/1001/media/private.avif",
		Visibility:  variables.MediaVisibilityPublic,
		CreatedAt:   now,
		UpdatedAt:   now,
	}))

	// 1. Rejects private user media attached to an article -> 403 Forbidden
	errPriv := media_service.ValidateAndActivateAdminArticleMedia(ctx, 5999, 5002, nil)
	var appErr *numan_error.AppError
	require.ErrorAs(t, errPriv, &appErr)
	assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)

	// 2. Rejects semantic mismatch (image passed as markdown_media_id) -> 400 Bad Request
	errSemantic := media_service.ValidateAndActivateAdminArticleMedia(ctx, 5001, 5001, nil)
	require.ErrorAs(t, errSemantic, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)

	// 3. Rejects semantic mismatch (markdown passed as cover_media_id) -> 400 Bad Request
	errSemantic2 := media_service.ValidateAndActivateAdminArticleMedia(ctx, 5002, 5002, nil)
	require.ErrorAs(t, errSemantic2, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)

	// 4. Cross-admin activation succeeds and promotes all assets to Public (1) without quarantine
	require.NoError(t, media_service.ValidateAndActivateAdminArticleMedia(ctx, 5001, 5002, []int64{5003}))

	for _, id := range []int64{5001, 5002, 5003} {
		mp, errGet := object_cache_service.GetMediaFromObjectCache(ctx, id)
		require.NoError(t, errGet)
		assert.Equal(t, variables.MediaVisibilityPublic, mp.Visibility)
	}
}

func TestDeactivateAdminMediaBatch_SharedMediaProtection(t *testing.T) {
	srv := test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()
	now := domain.NowMillis()

	// Create 2 public article media
	for _, id := range []int64{6001, 6002} {
		require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
			ID:          id,
			OwnerID:     9001,
			StoragePath: "articles/asset_" + strconv.FormatInt(id, 10) + ".avif",
			Visibility:  variables.MediaVisibilityPublic,
			CreatedAt:   now,
			UpdatedAt:   now,
		}))
	}

	// Another active article (ID=888) in L1 references media 6001
	otherArt := article_models.ArticlePayload{
		ID:                 888,
		Title:              "Autre Article",
		CoverMediaID:       6001,
		MarkdownMediaID:    7000,
		AttachmentMediaIDs: []int64{},
		Visibility:         variables.ArticleVisibilityPublished,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	rawOther, errM := msgpack.Marshal(otherArt)
	require.NoError(t, errM)
	srv.HashSet(variables.RedisKeyArticlesIndexData, "888", string(rawOther))

	// Deactivate [6001, 6002] when updating article 777 -> 6001 must be protected, only 6002 soft-deleted
	deactivated, errDeact := media_service.DeactivateAdminMediaBatch(ctx, []int64{6001, 6002}, 777)
	require.NoError(t, errDeact)
	assert.Equal(t, []int64{6002}, deactivated)

	// 6001 is still Public
	m6001, err1 := object_cache_service.GetMediaFromObjectCache(ctx, 6001)
	require.NoError(t, err1)
	assert.Equal(t, variables.MediaVisibilityPublic, m6001.Visibility)

	// 6002 is Soft-Deleted (GetMediaFromObjectCache returns 404 for soft-deleted)
	_, err2 := object_cache_service.GetMediaFromObjectCache(ctx, 6002)
	assert.Error(t, err2)
}

func TestRemovePhysicalMedia_BucketResolutionAndProtectedPrefixes(t *testing.T) {
	fakeMinio, minioClient := test.SetupTestMinio(t)
	infraMinio.MinioClient = minioClient
	t.Setenv("MINIO_BUCKET_NAME", "numan-bucket")
	t.Setenv("MINIO_PUBLIC_BUCKET_NAME", "numan-public")

	ctx := context.Background()

	fakeMinio.SeedObject("numan-public", "articles/a1.avif", []byte("pub"))
	fakeMinio.SeedObject("numan-public", "maps/tile.png", []byte("map"))
	fakeMinio.SeedObject("numan-public", "static/logo.png", []byte("static"))
	fakeMinio.SeedObject("numan-bucket", "users/1/media/u1.avif", []byte("priv"))

	// 1. Protected prefixes maps/ and static/ are never removed
	require.NoError(t, media_service.RemovePhysicalMedia(ctx, "maps/tile.png"))
	assert.NotEmpty(t, fakeMinio.Buckets["numan-public"]["maps/tile.png"])

	require.NoError(t, media_service.RemovePhysicalMedia(ctx, "static/logo.png"))
	assert.NotEmpty(t, fakeMinio.Buckets["numan-public"]["static/logo.png"])

	// 2. articles/ removed from numan-public
	require.NoError(t, media_service.RemovePhysicalMedia(ctx, "articles/a1.avif"))
	assert.Empty(t, fakeMinio.Buckets["numan-public"]["articles/a1.avif"])

	// 3. users/ removed from numan-bucket
	require.NoError(t, media_service.RemovePhysicalMedia(ctx, "users/1/media/u1.avif"))
	assert.Empty(t, fakeMinio.Buckets["numan-bucket"]["users/1/media/u1.avif"])
}
