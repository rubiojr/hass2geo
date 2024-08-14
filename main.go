package main

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
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

	var err error
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=query_only(1)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", dbPath))
	return db, err
}

func main() {
	app := &cli.App{
		Name:  "hass2geo",
		Usage: "make an explosive entrance",
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
					switch cCtx.String("format") {
					case "gpx":
						return exportGPX(db, cCtx.String("sensor-id"))
					case "geojson":
						return exportGeoJSON(db, cCtx.String("sensor-id"))
					default:
						return errors.New("Unsupported format")
					}
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}

}

func exportGeoJSON(db *sql.DB, sensor string) error {
	fc := geojson.NewFeatureCollection()

	rows, err := db.Query("select states.last_updated_ts, state_attributes.shared_attrs from state_attributes inner join states on state_attributes.attributes_id=states.attributes_id where states.metadata_id = ? order by states.last_updated_ts asc;", sensor)
	if err != nil {
		return err
	}
	added := 0
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
		if len(geo.Location) == 0 {
			continue
		}
		slices.Reverse(geo.Location)
		feat := geojson.NewPointFeature(geo.Location)
		feat.SetProperty("Name", geo.Name)
		feat.SetProperty("Country", geo.Country)
		feat.SetProperty("ISO Country", geo.ISOCountryCode)
		feat.SetProperty("Timestamp", ts)
		feat.SetProperty("Date", mt.Format("2006-01-02 15:04:05"))
		fc.AddFeature(feat)
		added++
	}

	if added == 0 {
		return errors.New("no points found")
	}

	rawJSON, err := fc.MarshalJSON()
	if err != nil {
		return err
	}
	fmt.Println(string(rawJSON))
	return nil
}

func exportGPX(db *sql.DB, sensor string) error {
	rows, err := db.Query("select states.last_updated_ts, state_attributes.shared_attrs from state_attributes inner join states on state_attributes.attributes_id=states.attributes_id where states.metadata_id = ? order by states.last_updated_ts asc;", sensor)
	if err != nil {
		return err
	}

	g := &gpx.GPX{
		Version: "1.0",
		Creator: "GPX Generator",
		Wpt:     []*gpx.WptType{},
	}

	added := 0
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
		if len(geo.Location) == 0 {
			continue
		}

		g.Wpt = append(g.Wpt, &gpx.WptType{
			Lat:  geo.Location[0],
			Lon:  geo.Location[1],
			Time: mt,
			Name: geo.Name,
		})
		added++
	}

	if added == 0 {
		return errors.New("no points found")
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
