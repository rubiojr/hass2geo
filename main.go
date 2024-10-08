package main

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"slices"
	"time"

	geojson "github.com/paulmach/go.geojson"
	"github.com/twpayne/go-gpx"
	"github.com/urfave/cli/v2"
	_ "modernc.org/sqlite"
)

func initDb(cCtx *cli.Context) (*sql.DB, error) {
	dbPath := cCtx.String("db")

	if dbPath == "" {
		return nil, fmt.Errorf("database file is required.")
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("database file does not exist.")
	}

	var err error
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=query_only(1)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", dbPath))
	return db, err
}

func main() {
	app := &cli.App{
		Name:    "hass2geo",
		Usage:   "make an explosive entrance",
		Version: Version,
		Action: func(*cli.Context) error {
			return nil
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "db",
				Value:    "",
				Usage:    "SQLite database file",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "format",
				Value:    "gpx",
				Usage:    "Output format",
				Required: false,
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "sensors",
				Usage: "List available sensors",
				Action: func(cCtx *cli.Context) error {
					db, err := initDb(cCtx)
					if err != nil {
						return err
					}
					sensors, err := findSensors(db)
					if err != nil {
						return err
					}
					for _, sensor := range sensors {
						fmt.Printf("[%d] %s\n", sensor.MetadataId, sensor.Name)
					}

					return nil
				},
			},
			{
				Name:  "export",
				Usage: "Export to a given format",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "format",
						Value:    "gpx",
						Usage:    "Export format",
						Required: false,
					},
					&cli.StringFlag{
						Name:     "filter-by-country",
						Usage:    "Only export locations in a given country ISO code",
						Required: false,
					},
					&cli.StringFlag{
						Name:     "sensor-id",
						Usage:    "Sensor ID to export",
						Required: true,
					},
				},
				Action: func(cCtx *cli.Context) error {
					db, err := initDb(cCtx)
					if err != nil {
						return err
					}
					return export(db, cCtx.String("sensor-id"), cCtx.String("format"), cCtx.String("filter-by-country"))
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func export(db *sql.DB, sensor string, format string, filter string) error {
	rows, err := db.Query("select states.last_updated_ts, state_attributes.shared_attrs from state_attributes inner join states on state_attributes.attributes_id=states.attributes_id where states.metadata_id = ? order by states.last_updated_ts asc;", sensor)
	if err != nil {
		return err
	}
	added := 0
	var locations []*GeoInfo
	for rows.Next() {
		var s string
		var ts float64

		if err := rows.Scan(&ts, &s); err != nil {
			return err
		}

		mt := time.Unix(int64(ts), 0)
		geo, err := decodeRow(s)
		if err != nil {
			return err
		}
		geo.Timestamp = &mt

		if filter != "" && geo.ISOCountryCode != filter {
			continue
		}

		if len(geo.Location) == 0 {
			continue
		}
		locations = append(locations, geo)
		added++
	}

	switch format {
	case "gpx":
		return exportGPX(locations)
	case "geojson":
		return exportGeoJSON(locations)
	}
	return fmt.Errorf("unsupported format: %s", format)
}

func exportGeoJSON(locations []*GeoInfo) error {
	fc := geojson.NewFeatureCollection()

	for _, geo := range locations {
		slices.Reverse(geo.Location)
		feat := geojson.NewPointFeature(geo.Location)
		feat.SetProperty("Name", geo.Name)
		feat.SetProperty("Country", geo.Country)
		feat.SetProperty("ISO Country", geo.ISOCountryCode)
		feat.SetProperty("Timestamp", geo.Timestamp.Unix())
		feat.SetProperty("Time", geo.Timestamp)
		feat.SetProperty("Date", geo.Timestamp.Format("2006-01-02 15:04:05"))
		fc.AddFeature(feat)
	}

	rawJSON, err := fc.MarshalJSON()
	if err != nil {
		return err
	}

	fmt.Println(string(rawJSON))
	return nil
}

func exportGPX(locations []*GeoInfo) error {
	g := &gpx.GPX{
		Version: "1.0",
		Creator: "GPX Generator",
		Wpt:     []*gpx.WptType{},
	}

	for _, geo := range locations {
		g.Wpt = append(g.Wpt, &gpx.WptType{
			Lat:  geo.Location[0],
			Lon:  geo.Location[1],
			Time: *geo.Timestamp,
			Name: geo.Name,
		})
	}

	fmt.Print(xml.Header)
	g.WriteIndent(os.Stdout, "", "  ")

	return nil
}

func findSensors(db *sql.DB) ([]Sensor, error) {
	rows, err := db.Query(`select metadata_id,entity_id,replace(replace(entity_id,"_geocoded_location", ""), "sensor.","") from states_meta where entity_id like 'sensor.%_geocoded_location'`)
	if err != nil {
		return nil, err
	}

	var sensors []Sensor
	for rows.Next() {
		var sensor string
		var metadataId int64
		var entityId string
		if err := rows.Scan(&metadataId, &entityId, &sensor); err != nil {
			return nil, err
		}
		sensors = append(sensors, Sensor{Name: sensor, EntityId: entityId, MetadataId: metadataId})
	}
	return sensors, nil
}

func decodeRow(row string) (*GeoInfo, error) {
	var geo GeoInfo
	return &geo, json.Unmarshal([]byte(row), &geo)
}
