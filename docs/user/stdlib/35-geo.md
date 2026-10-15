# geo

Geodetic calculations on the sphere (1.27.0): great-circle distance, initial
bearing, midpoint, and destination point. All coordinates are in decimal
degrees. Distances use a mean Earth radius of 6371 km.

```gb
import geo;
import io;

let d = geo.haversineDistance(51.5074, -0.1278, 48.8566, 2.3522);
io.println("${d} km");   /* ~343.56 km (London to Paris) */
```

## Functions

### haversineDistance

```gb
geo.haversineDistance(lat1, lon1, lat2, lon2, unit?)
```

Returns the great-circle distance between two points using the Haversine
formula. `lat1`, `lon1`, `lat2`, `lon2` are in decimal degrees. `unit` is
an optional string defaulting to `"km"`.

```gb
geo.haversineDistance(51.5074, -0.1278, 48.8566, 2.3522);         /* km */
geo.haversineDistance(51.5074, -0.1278, 48.8566, 2.3522, "mi");   /* miles */
geo.haversineDistance(51.5074, -0.1278, 48.8566, 2.3522, "m");    /* metres */
geo.haversineDistance(51.5074, -0.1278, 48.8566, 2.3522, "nmi");  /* nautical miles */
```

### bearing

```gb
geo.bearing(lat1, lon1, lat2, lon2)
```

Returns the initial bearing in degrees (0 to 360, clockwise from north)
from point 1 to point 2.

```gb
geo.bearing(51.5074, -0.1278, 48.8566, 2.3522);  /* ~148.12 degrees */
```

### midpoint

```gb
geo.midpoint(lat1, lon1, lat2, lon2)
```

Returns the geographic midpoint on the great-circle path as a dict
`{"lat": ..., "lon": ...}` in decimal degrees.

```gb
let mid = geo.midpoint(51.5074, -0.1278, 48.8566, 2.3522);
mid["lat"];   /* ~50.19 */
mid["lon"];   /* ~1.15 */
```

### destination

```gb
geo.destination(lat, lon, bearing, distance, unit?)
```

Returns the destination point `{"lat": ..., "lon": ...}` reached by
travelling `distance` at the given `bearing` (degrees, clockwise from north)
from the starting point `(lat, lon)`. `unit` defaults to `"km"`.

```gb
let dest = geo.destination(51.5074, -0.1278, 90.0, 100.0);
/* point 100 km due east of London */

geo.destination(51.5074, -0.1278, 180.0, 200.0, "mi");
/* 200 miles due south */
```

## Distance units

| Unit string | Meaning |
|-------------|---------|
| `"km"` (default) | kilometres |
| `"m"` | metres |
| `"mi"` | statute miles |
| `"nmi"` | nautical miles |

## Error handling

- A latitude outside `[-90, 90]` raises `RuntimeError` with the message
  `<function>: latitude must be in [-90, 90]`.
- An unrecognised unit string raises `RuntimeError` with the message
  `geo: unknown distance unit "..."`.

```gb
try {
    geo.haversineDistance(91.0, 0.0, 0.0, 0.0);
} catch (RuntimeError e) {
    io.println(e.message); /* geo.haversineDistance: latitude must be in [-90, 90] */
}
```
