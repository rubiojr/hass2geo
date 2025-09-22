package main

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=query_only(1)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", dbPath))
	return db, err
}

func main() {
	app := &cli.App{
		Name:    "hass2geo",
		Usage:   "Export Home Assistant geocoded location state history to geo formats",
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
				Usage:    "Output format (gpx|geojson)",
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
						fmt.Printf("[%d] %s (%s)\n", sensor.MetadataId, sensor.Name, sensor.EntityId)
					}

					return nil
				},
			},
			{
				Name:  "export",
				Usage: "Export a sensor history to a given format",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "format",
						Value:    "gpx",
						Usage:    "Export format (gpx|geojson)",
						Required: false,
					},
					&cli.StringFlag{
						Name:     "filter-by-country",
						Usage:    "Only export locations in a given ISO country code",
						Required: false,
					},
					&cli.StringFlag{
						Name:     "sensor-id",
						Usage:    "Sensor metadata ID to export (see sensors command)",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "save",
						Usage:    "Directory path. If set, output is written to an auto-named file instead of stdout",
						Required: false,
					},
				},
				Action: func(cCtx *cli.Context) error {
					db, err := initDb(cCtx)
					if err != nil {
						return err
					}
					return export(
						db,
						cCtx.String("sensor-id"),
						cCtx.String("format"),
						cCtx.String("filter-by-country"),
						cCtx.String("save"),
					)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func export(db *sql.DB, sensor string, format string, filter string, saveDir string) error {
	rows, err := db.Query(`
		select states.last_updated_ts, state_attributes.shared_attrs
		from state_attributes
		inner join states on state_attributes.attributes_id = states.attributes_id
		where states.metadata_id = ?
		order by states.last_updated_ts asc;`, sensor)
	if err != nil {
		return err
	}
	defer rows.Close()

	var locations []*GeoInfo
	for rows.Next() {
		var attrJSON string
		var ts float64

		if err := rows.Scan(&ts, &attrJSON); err != nil {
			return err
		}

		mt := time.Unix(int64(ts), 0)
		geo, err := decodeRow(attrJSON)
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
	}

	var data []byte
	switch format {
	case "gpx":
		data, err = marshalGPX(locations)
	case "geojson":
		data, err = marshalGeoJSON(locations)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
	if err != nil {
		return err
	}

	// If --save was not provided, write to stdout (existing behavior)
	if saveDir == "" {
		_, err = os.Stdout.Write(data)
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	sName, err := sensorNameByMetadataID(db, sensor)
	if err != nil || sName == "" {
		// Fallback to sensor id if we can't resolve a friendly name
		sName = sensor
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s.%s", sName, timestamp, format)
	fullPath := filepath.Join(saveDir, filename)

	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Saved export to %s\n", fullPath)
	return nil
}

func marshalGeoJSON(locations []*GeoInfo) ([]byte, error) {
	fc := geojson.NewFeatureCollection()

	for _, geo := range locations {
		// Original code reversed the coordinate order per feature (mutating slice)
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
		return nil, err
	}
	return rawJSON, nil
}

func marshalGPX(locations []*GeoInfo) ([]byte, error) {
	g := &gpx.GPX{
		Version: "1.0",
		Creator: "hass2geo",
		Wpt:     []*gpx.WptType{},
	}

	for _, geo := range locations {
		// In original code: geo.Location[0] = lat, [1] = lon
		if len(geo.Location) < 2 {
			continue
		}
		g.Wpt = append(g.Wpt, &gpx.WptType{
			Lat:  geo.Location[0],
			Lon:  geo.Location[1],
			Time: *geo.Timestamp,
			Name: geo.Name,
		})
	}

	var sb strings.Builder
	sb.WriteString(xml.Header)
	if err := g.WriteIndent(&sb, "", "  "); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

func sensorNameByMetadataID(db *sql.DB, metadataID string) (string, error) {
	row := db.QueryRow(`
		select replace(replace(entity_id,"_geocoded_location",""), "sensor.","") as sensor_name
		from states_meta
		where metadata_id = ?
		limit 1;
	`, metadataID)

	var name string
	if err := row.Scan(&name); err != nil {
		return "", err
	}
	return name, nil
}

func findSensors(db *sql.DB) ([]Sensor, error) {
	rows, err := db.Query(`select metadata_id,entity_id,replace(replace(entity_id,"_geocoded_location", ""), "sensor.","") from states_meta where entity_id like 'sensor.%_geocoded_location'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
