package creativescene

import (
	"math"
	"time"
)

// The sun reference (W2, Epic revision v2): what the assistant cannot see,
// since the renderer works out where a physical sky's sun is. It comes with
// a read of the scene and with an edit that wrote the environment, is never
// stored, and never fingerprints. NOAA's solar calculator
// (gml.noaa.gov/grad/solcalc, after Meeus), the source the renderer's
// astronomy-engine was checked against: they agree to 0.02° and seconds.

// SunReport is the sun of a physical sky placed by location: at its local
// time, and over its local day. Times are HH:MM in the sky's time zone.
type SunReport struct {
	// ElevationDeg is the apparent elevation, refraction included.
	ElevationDeg float64 `json:"elevation_deg"`
	// CompassDeg is clockwise from north.
	CompassDeg float64 `json:"compass_deg"`
	// SceneAzimuthDeg is the document's azimuth, sun_azimuth_deg's
	// convention with north_azimuth_deg counted in.
	SceneAzimuthDeg  float64  `json:"scene_azimuth_deg"`
	SolarNoon        string   `json:"solar_noon"`
	NoonElevationDeg float64  `json:"noon_elevation_deg"`
	Sunrise          string   `json:"sunrise,omitempty"`
	Sunset           string   `json:"sunset,omitempty"`
	Polar            string   `json:"polar,omitempty"`
	Morning          SunSpans `json:"morning"`
	Evening          SunSpans `json:"evening"`
}

// SunSpans are the day's stretches, [from, to], those it has.
type SunSpans struct {
	BlueHour   []string `json:"blue_hour,omitempty"`
	GoldenHour []string `json:"golden_hour,omitempty"`
}

// The geometric elevations of the sun's centre that bound the day's
// stretches, as the frontend's (world/astro.ts): sunrise and sunset at
// −0.833° (NOAA: refraction and the sun's radius), the blue hour from −6° to
// −4°, the golden hour from −4° to 6°.
const (
	horizonDeg     = -0.833
	blueHourFrom   = -6.0
	goldenHourFrom = -4.0
	goldenHourTo   = 6.0
)

// sunReport is the sun reference of sky, nil unless it is a physical sky
// whose sun comes from its place and time. sky is complete and valid.
func sunReport(sky Sky) *SunReport {
	if sky.Kind != SkyPhysical || sky.SunMode == nil || *sky.SunMode != "location" || sky.Date == nil {
		return nil
	}
	loc, err := time.LoadLocation(*sky.Timezone)
	if err != nil {
		return nil
	}
	lat, lon := *sky.LatitudeDeg, *sky.LongitudeDeg
	now, err1 := localToUTC(*sky.Date, *sky.LocalTime, loc)
	start, err2 := localToUTC(*sky.Date, "00:00", loc)
	if err1 != nil || err2 != nil {
		return nil
	}
	end, err := localToUTC(start.In(loc).AddDate(0, 0, 1).Format(time.DateOnly), "00:00", loc)
	if err != nil {
		return nil
	}
	sun := sunAt(now, lat, lon)
	r := &SunReport{ElevationDeg: round1(sun.apparent), CompassDeg: round1(sun.compass), SceneAzimuthDeg: round1(documentAzimuth(sun.compass, *sky.NorthAzimuthDeg))}

	// The day minute by minute: crossings are interpolated between minutes,
	// noon is the highest minute refined on a parabola. Polar days and
	// elevations the sun never reaches need no special case.
	n := int(end.Sub(start) / time.Minute)
	elevations := make([]float64, n+1)
	for i := range elevations {
		elevations[i] = sunAt(start.Add(time.Duration(i)*time.Minute), lat, lon).geometric
	}
	at := func(minute float64) time.Time { return start.Add(time.Duration(minute * float64(time.Minute))) }
	clock := func(t time.Time) string { return t.In(loc).Round(time.Minute).Format("15:04") }
	crossing := func(rising bool, deg float64) (float64, bool) {
		for i := 1; i <= n; i++ {
			a, b := elevations[i-1], elevations[i]
			if rising && a < deg && b >= deg || !rising && a >= deg && b < deg {
				return float64(i-1) + (deg-a)/(b-a), true
			}
		}
		return 0, false
	}
	span := func(from, to float64, okFrom, okTo bool) []string {
		if !okFrom || !okTo || from >= to {
			return nil
		}
		return []string{clock(at(from)), clock(at(to))}
	}
	high := 0
	for i, e := range elevations {
		if e > elevations[high] {
			high = i
		}
	}
	noon := float64(high)
	if high > 0 && high < n {
		a, b, c := elevations[high-1], elevations[high], elevations[high+1]
		if d := a - 2*b + c; d != 0 {
			noon += (a - c) / (2 * d)
		}
	}
	r.SolarNoon = clock(at(noon))
	r.NoonElevationDeg = round1(sunAt(at(noon), lat, lon).apparent)

	rise, okRise := crossing(true, horizonDeg)
	set, okSet := crossing(false, horizonDeg)
	if okRise {
		r.Sunrise = clock(at(rise))
	}
	if okSet {
		r.Sunset = clock(at(set))
	}
	if !okRise && !okSet {
		r.Polar = "night"
		if elevations[high] > horizonDeg {
			r.Polar = "day"
		}
	}
	blueFrom, ok1 := crossing(true, blueHourFrom)
	goldenFrom, ok2 := crossing(true, goldenHourFrom)
	goldenTo, ok3 := crossing(true, goldenHourTo)
	r.Morning = SunSpans{BlueHour: span(blueFrom, goldenFrom, ok1, ok2), GoldenHour: span(goldenFrom, goldenTo, ok2, ok3)}
	goldenFrom, ok1 = crossing(false, goldenHourTo)
	goldenTo, ok2 = crossing(false, goldenHourFrom)
	blueTo, ok3 := crossing(false, blueHourFrom)
	r.Evening = SunSpans{GoldenHour: span(goldenFrom, goldenTo, ok1, ok2), BlueHour: span(goldenTo, blueTo, ok2, ok3)}
	return r
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// documentAzimuth turns a compass azimuth into the document's: sun_azimuth_deg
// grows counter-clockwise seen from above, the compass clockwise.
func documentAzimuth(compassDeg, northAzimuthDeg float64) float64 {
	return math.Mod(math.Mod(northAzimuthDeg-compassDeg, 360)+360, 360)
}

// localToUTC is a local date and time in loc as an instant, as Temporal's
// 'compatible' and the frontend's localToUtcMs: a time the clocks skip moves
// forward by the gap, a repeated time is the earlier one. time.Date leaves
// which side of a gap it picks unspecified.
func localToUTC(date, localTime string, loc *time.Location) (time.Time, error) {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return time.Time{}, err
	}
	clock, err := time.Parse("15:04", localTime)
	if err != nil {
		return time.Time{}, err
	}
	wall := time.Date(d.Year(), d.Month(), d.Day(), clock.Hour(), clock.Minute(), 0, 0, time.UTC)
	offset := func(t time.Time) time.Duration {
		_, s := t.In(loc).Zone()
		return time.Duration(s) * time.Second
	}
	before, after := offset(wall.Add(-24*time.Hour)), offset(wall.Add(24*time.Hour))
	var best time.Time
	for _, o := range []time.Duration{before, after} {
		if t := wall.Add(-o); offset(t) == o && (best.IsZero() || t.Before(best)) {
			best = t
		}
	}
	if best.IsZero() {
		// In a gap neither offset gives the wall clock back: the one before the jump moves it forward.
		best = wall.Add(-before)
	}
	return best, nil
}

// sunPosition is where the sun is from a place at sea level: compass
// azimuth, and elevation with and without refraction, in degrees.
type sunPosition struct{ compass, apparent, geometric float64 }

// sunAt is NOAA's calcAzEl at an instant (UT, no ΔT, as NOAA).
func sunAt(at time.Time, latitudeDeg, longitudeDeg float64) sunPosition {
	jd := float64(at.UnixMilli())/86_400_000 + 2440587.5
	t := (jd - 2451545.0) / 36525.0
	eqTime, decl := equationOfTime(t), declination(t)
	utcMinutes := float64(at.UTC().Hour()*60+at.UTC().Minute()) + float64(at.UTC().Second())/60 + float64(at.UTC().Nanosecond())/6e10
	trueSolar := math.Mod(utcMinutes+eqTime+4*longitudeDeg, 1440)
	if trueSolar < 0 {
		trueSolar += 1440
	}
	hourAngle := trueSolar/4 - 180
	lat, dec := deg2rad(latitudeDeg), deg2rad(decl)
	csz := math.Sin(lat)*math.Sin(dec) + math.Cos(lat)*math.Cos(dec)*math.Cos(deg2rad(hourAngle))
	zenith := rad2deg(math.Acos(math.Max(-1, math.Min(1, csz))))
	var azimuth float64
	if den := math.Cos(lat) * math.Sin(deg2rad(zenith)); math.Abs(den) > 0.001 {
		az := (math.Sin(lat)*math.Cos(deg2rad(zenith)) - math.Sin(dec)) / den
		azimuth = 180 - rad2deg(math.Acos(math.Max(-1, math.Min(1, az))))
		if hourAngle > 0 {
			azimuth = -azimuth
		}
	} else if latitudeDeg > 0 {
		azimuth = 180
	}
	if azimuth < 0 {
		azimuth += 360
	}
	geometric := 90 - zenith
	return sunPosition{compass: azimuth, apparent: geometric + refraction(geometric), geometric: geometric}
}

func deg2rad(d float64) float64 { return d * math.Pi / 180 }
func rad2deg(r float64) float64 { return r * 180 / math.Pi }

// declination and equationOfTime (minutes) of the sun at t Julian centuries since J2000.
func declination(t float64) float64 {
	return rad2deg(math.Asin(math.Sin(deg2rad(obliquity(t))) * math.Sin(deg2rad(apparentLongitude(t)))))
}

func equationOfTime(t float64) float64 {
	epsilon, l0, e, m := obliquity(t), meanLongitude(t), eccentricity(t), meanAnomaly(t)
	y := math.Tan(deg2rad(epsilon) / 2)
	y *= y
	l0r, mr := deg2rad(l0), deg2rad(m)
	etime := y*math.Sin(2*l0r) - 2*e*math.Sin(mr) + 4*e*y*math.Sin(mr)*math.Cos(2*l0r) - 0.5*y*y*math.Sin(4*l0r) - 1.25*e*e*math.Sin(2*mr)
	return rad2deg(etime) * 4
}

func meanLongitude(t float64) float64 {
	return math.Mod(math.Mod(280.46646+t*(36000.76983+t*0.0003032), 360)+360, 360)
}

func meanAnomaly(t float64) float64  { return 357.52911 + t*(35999.05029-0.0001537*t) }
func eccentricity(t float64) float64 { return 0.016708634 - t*(0.000042037+0.0000001267*t) }

func apparentLongitude(t float64) float64 {
	m := deg2rad(meanAnomaly(t))
	center := math.Sin(m)*(1.914602-t*(0.004817+0.000014*t)) + math.Sin(2*m)*(0.019993-0.000101*t) + math.Sin(3*m)*0.000289
	omega := 125.04 - 1934.136*t
	return meanLongitude(t) + center - 0.00569 - 0.00478*math.Sin(deg2rad(omega))
}

func obliquity(t float64) float64 {
	seconds := 21.448 - t*(46.815+t*(0.00059-t*0.001813))
	omega := 125.04 - 1934.136*t
	return 23 + (26+seconds/60)/60 + 0.00256*math.Cos(deg2rad(omega))
}

// refraction is NOAA's approximation, in degrees, for a geometric elevation.
func refraction(elevation float64) float64 {
	if elevation > 85 {
		return 0
	}
	te := math.Tan(deg2rad(elevation))
	var arcsec float64
	switch {
	case elevation > 5:
		arcsec = 58.1/te - 0.07/(te*te*te) + 0.000086/(te*te*te*te*te)
	case elevation > -0.575:
		arcsec = 1735 + elevation*(-518.2+elevation*(103.4+elevation*(-12.79+elevation*0.711)))
	default:
		arcsec = -20.774 / te
	}
	return arcsec / 3600
}
