# Home Assistant Geolocation Exporter

Exports geolocation data from your Home Assistant SQLite database.

Home Assistant can track your device (phone, table, etc) location if you have location sharing enabled in your Home Assistant companion app. See [Home Assistant location documentation](https://companion.home-assistant.io/docs/core/location/).

GeoJSON and GPX formats are supported.

## Installation

Download the latest release from the [releases page](https://github.com/rubiojr/hass2geo/releases) and extract the binary to a directory in your PATH.

## Usage

You'll need direct access to Home Assistant's [SQLite database](https://www.home-assistant.io/docs/backend/database/). The database is typically located at `config/home-assistant_v2.db`.

You will also need the device ID you want to export location data from. You can find the device ID using the `sensors` subcommand:

```
hass2geo --db home-assistant_v2.db sensors
```

That will print a list of IDs and sensors like:

```
[5236] micro
[5246] mobster
[5255] mini
[5320] ipad
```

You can then use the `export` subcommand to export the location data:

```
hass2geo --db home-assistant_v2.db export --sensor-id 5236 --format geojson # exports `micro` location data
```

## Related software

- [Timelinize](https://github.com/timelinize/timelinize) - Can import geojson and gpx files exported with hass2eo and render a timeline.
