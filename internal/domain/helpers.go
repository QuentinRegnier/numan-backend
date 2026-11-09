package domain

import "time"

// TimeToMillis convertit un time.Time en millisecondes Unix.
func TimeToMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func TimeToSeconds(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// MillisToTime convertit un timestamp millisecondes Unix en time.Time UTC.
func MillisToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// NowMillis retourne le timestamp actuel en millisecondes UTC.
func NowMillis() int64 {
	return time.Now().UTC().UnixMilli()
}

// NowTime retourne l'heure actuelle en UTC.
func NowTime() time.Time {
	return time.Now().UTC()
}
