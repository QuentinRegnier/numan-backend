package map_models

// DeletePOIInput contient la liste des identifiants de POIs à supprimer (`[]POI_IDs`).
type DeletePOIInput struct {
	POIIDs []int64 `json:"poi_ids" binding:"required,min=1"`
}

// DeletePOIOutput confirme la suppression synchrone L1 et l'ordonnancement asynchrone L3 des POIs.
type DeletePOIOutput struct {
	DeletedCount int     `json:"deleted_count"`
	POIIDs       []int64 `json:"poi_ids"`
}
