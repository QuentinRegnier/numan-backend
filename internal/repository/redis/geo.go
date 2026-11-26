package redis

import (
	"context"
	"fmt"
	"math"
	"strconv"

	goredis "github.com/redis/go-redis/v9"
)

// GeoLocationItem représente un point géographique indexé dans une collection GEO Redis.
type GeoLocationItem struct {
	MemberID  int64
	Longitude float64
	Latitude  float64
}

// GeoAdd indexe un point géographique unique (longitude, latitude) dans le sous-ensemble `id` de la collection.
func (c *Collection) GeoAdd(ctx context.Context, id any, longitude, latitude float64, member any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	return c.client.GeoAdd(ctx, c.Key(id), &goredis.GeoLocation{
		Name:      fmt.Sprintf("%v", member),
		Longitude: longitude,
		Latitude:  latitude,
	}).Err()
}

// GeoAddBatch indexe un lot de points géographiques en une seule commande atomique `GEOADD`.
func (c *Collection) GeoAddBatch(ctx context.Context, id any, items []GeoLocationItem) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	locations := make([]*goredis.GeoLocation, 0, len(items))
	for _, it := range items {
		locations = append(locations, &goredis.GeoLocation{
			Name:      strconv.FormatInt(it.MemberID, 10),
			Longitude: it.Longitude,
			Latitude:  it.Latitude,
		})
	}
	return c.client.GeoAdd(ctx, c.Key(id), locations...).Err()
}

// GeoSearchBox recherche les identifiants situés dans une boîte englobante centrée sur (centerLon, centerLat)
// de dimensions (widthKm x heightKm) en kilomètres via `GEOSEARCH ... FROMLONLAT ... BYBOX`.
func (c *Collection) GeoSearchBox(ctx context.Context, id any, centerLon, centerLat, widthKm, heightKm float64, count int) ([]string, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	if widthKm <= 0 {
		widthKm = 0.01
	}
	if heightKm <= 0 {
		heightKm = 0.01
	}
	query := &goredis.GeoSearchQuery{
		Longitude: centerLon,
		Latitude:  centerLat,
		BoxWidth:  widthKm,
		BoxHeight: heightKm,
		BoxUnit:   "km",
		Sort:      "ASC",
		Count:     count,
		CountAny:  false,
	}
	return c.client.GeoSearch(ctx, c.Key(id), query).Result()
}

// GeoSearchBBox convertit une Bounding Box WGS84 (SWLat, SWLng, NELat, NELng) en requête `GEOSEARCH BYBOX`
// avec une marge sphérique de sécurité pour éviter toute perte aux bords de projection.
func (c *Collection) GeoSearchBBox(ctx context.Context, id any, swLat, swLng, neLat, neLng float64, count int) ([]int64, error) {
	centerLat, centerLon, widthKm, heightKm := BBoxToGeoBox(swLat, swLng, neLat, neLng)
	members, err := c.GeoSearchBox(ctx, id, centerLon, centerLat, widthKm, heightKm, count)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(members))
	for _, m := range members {
		parsedID, errParse := strconv.ParseInt(m, 10, 64)
		if errParse == nil && parsedID > 0 {
			ids = append(ids, parsedID)
		}
	}
	return ids, nil
}

// GeoRem supprime un ou plusieurs membres de l'index GEO Redis (stocké sous forme de ZSET sous-jacent).
func (c *Collection) GeoRem(ctx context.Context, id any, members ...any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	return c.client.ZRem(ctx, c.Key(id), members...).Err()
}

// BBoxToGeoBox calcule le centre (lat, lon) et les dimensions (widthKm, heightKm) d'une Bounding Box WGS84.
// Applique un facteur de sécurité de 5% (+50m minimum) pour compenser la courbure terrestre dans GEOSEARCH BYBOX.
func BBoxToGeoBox(swLat, swLng, neLat, neLng float64) (centerLat, centerLon, widthKm, heightKm float64) {
	centerLat = (swLat + neLat) / 2.0
	centerLon = (swLng + neLng) / 2.0

	heightKm = haversineDistanceKm(swLat, centerLon, neLat, centerLon)
	// La largeur d'un degré de longitude est maximale à la latitude la plus proche de l'équateur
	widthSw := haversineDistanceKm(swLat, swLng, swLat, neLng)
	widthNe := haversineDistanceKm(neLat, swLng, neLat, neLng)
	widthCenter := haversineDistanceKm(centerLat, swLng, centerLat, neLng)
	widthKm = math.Max(widthSw, math.Max(widthNe, widthCenter))

	// Marge de sécurité de 5% + 0.05 km (50m) pour garantir l'inclusion des points sur la frontière exacte
	widthKm = widthKm*1.05 + 0.05
	heightKm = heightKm*1.05 + 0.05
	return centerLat, centerLon, widthKm, heightKm
}

func haversineDistanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0088
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	rLat1 := lat1 * math.Pi / 180.0
	rLat2 := lat2 * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rLat1)*math.Cos(rLat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}
