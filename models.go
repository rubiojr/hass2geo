package main

import (
	"encoding/json"
	"time"
)

type Sensor struct {
	Name       string
	EntityId   string
	MetadataId int64
}

type GeoInfo struct {
	AdministrativeArea    string          `json:"Administrative Area"`
	AreasOfInterest       AreasOfInterest `json:"Areas Of Interest"`
	Country               string          `json:"Country"`
	Locality              string          `json:"Locality"`
	PostalCode            string          `json:"Postal Code"`
	SubAdministrativeArea string          `json:"Sub Administrative Area"`
	SubLocality           string          `json:"Sub Locality"`
	Timezone              string          `json:"Time Zone"`
	Location              []float64       `json:"Location"`
	FriendlyName          string          `json:"friendly_name"`
	Thoroughfare          string          `json:"Thoroughfare"`
	InlandWater           string          `json:"Inland Water"`
	ISOCountryCode        string          `json:"ISO Country Code"`
	Name                  string          `json:"Name"`
	Ocean                 string          `json:"Ocean"`
	Timestamp             *time.Time      `json:"-"`
}

type AreasOfInterest struct {
	Areas []string
}

func (a *AreasOfInterest) UnmarshalJSON(b []byte) (err error) {
	arr, str := []string{}, ""
	if err = json.Unmarshal(b, &arr); err == nil {
		a.Areas = arr
		return
	}

	if err = json.Unmarshal(b, &str); err == nil {
		*a = AreasOfInterest{Areas: []string{str}}
		return
	}

	return
}
