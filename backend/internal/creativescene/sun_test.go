package creativescene

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"
	"time"
)

// W2: the sun reference the assistant reads (sun.go) against NOAA's solar
// calculator, the frontend's astronomy (world/astro.ts) and Python's
// zoneinfo; the data is shared with frontend/scripts/fixtures/astro.

func readJSON(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
}

func physical(fields map[string]any) Sky {
	s := Sky{Kind: SkyPhysical}
	raw, _ := json.Marshal(fields)
	if err := json.Unmarshal(raw, &s); err != nil {
		panic(err)
	}
	s.Kind = SkyPhysical
	if err := completeSky(&s); err != nil {
		panic(err)
	}
	return s
}

// minutes is HH:MM as minutes of the day.
func minutes(t *testing.T, hhmm string) int {
	t.Helper()
	h, err1 := strconv.Atoi(hhmm[:2])
	m, err2 := strconv.Atoi(hhmm[3:])
	if len(hhmm) != 5 || err1 != nil || err2 != nil {
		t.Fatalf("not HH:MM: %q", hhmm)
	}
	return h*60 + m
}

func TestLocalTimeIsCompatible(t *testing.T) {
	var data struct {
		Cases []struct {
			Timezone  string `json:"timezone"`
			Date      string `json:"date"`
			LocalTime string `json:"local_time"`
			UTC       string `json:"utc"`
			Note      string `json:"note"`
		} `json:"cases"`
	}
	readJSON(t, "local_time.json", &data)
	for _, c := range data.Cases {
		loc, err := time.LoadLocation(c.Timezone)
		if err != nil {
			t.Fatal(err)
		}
		got, err := localToUTC(c.Date, c.LocalTime, loc)
		if err != nil {
			t.Fatal(err)
		}
		if s := got.UTC().Format("2006-01-02T15:04:05.000Z"); s != c.UTC {
			t.Errorf("%s %s %s = %s, want %s (%s)", c.Timezone, c.Date, c.LocalTime, s, c.UTC, c.Note)
		}
	}
	if len(data.Cases) != 20 {
		t.Fatalf("%d cases", len(data.Cases))
	}
}

type noaaCase struct {
	Location  string   `json:"location"`
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	Timezone  string   `json:"timezone"`
	Date      string   `json:"date"`
	Sunrise   string   `json:"sunrise"`
	Sunset    string   `json:"sunset"`
	Points    [][3]any `json:"points"`
}

func TestSunMatchesNOAA(t *testing.T) {
	var data struct {
		Cases []noaaCase `json:"cases"`
	}
	readJSON(t, "noaa_sun.json", &data)
	rad := math.Pi / 180
	compared := 0
	for _, c := range data.Cases {
		for _, p := range c.Points {
			at, err := time.Parse(time.RFC3339, p[0].(string))
			if err != nil {
				t.Fatal(err)
			}
			wantEl, wantAz := p[1].(float64), p[2].(float64)
			if wantEl < 0 {
				continue
			}
			got := sunAt(at, c.Latitude, c.Longitude)
			cosSep := math.Sin(got.apparent*rad)*math.Sin(wantEl*rad) + math.Cos(got.apparent*rad)*math.Cos(wantEl*rad)*math.Cos((got.compass-wantAz)*rad)
			if sep := math.Acos(math.Min(1, cosSep)) / rad; sep > 0.05 {
				t.Errorf("%s %s: %.4f° from NOAA", c.Location, p[0], sep)
			}
			compared++
		}
		loc, _ := time.LoadLocation(c.Timezone)
		r := sunReport(physical(map[string]any{"date": c.Date, "latitude_deg": c.Latitude, "longitude_deg": c.Longitude, "timezone": c.Timezone}))
		for name, pair := range map[string][2]string{"sunrise": {r.Sunrise, c.Sunrise}, "sunset": {r.Sunset, c.Sunset}} {
			want, err := time.Parse(time.RFC3339, pair[1])
			if err != nil {
				t.Fatal(err)
			}
			if d := minutes(t, pair[0]) - minutes(t, want.In(loc).Round(time.Minute).Format("15:04")); d < -1 || d > 1 {
				t.Errorf("%s %s %s %s, NOAA %s", c.Location, c.Date, name, pair[0], want.In(loc).Format("15:04:05"))
			}
		}
	}
	if compared < 400 {
		t.Fatalf("compared %d points", compared)
	}
}

// The assistant's times agree with what the environment tab shows, to the minute.
func TestSunReportMatchesTheFrontend(t *testing.T) {
	var data struct {
		Sky              map[string]any `json:"sky"`
		Times            SunReport      `json:"times"`
		NoonElevationDeg float64        `json:"noon_elevation_deg"`
	}
	readJSON(t, "sun_times_frontend.json", &data)
	r := sunReport(physical(data.Sky))
	want := data.Times
	pairs := map[string][2]string{"solar noon": {r.SolarNoon, want.SolarNoon}, "sunrise": {r.Sunrise, want.Sunrise}, "sunset": {r.Sunset, want.Sunset}}
	spans := map[string][2][]string{
		"morning blue": {r.Morning.BlueHour, want.Morning.BlueHour}, "morning golden": {r.Morning.GoldenHour, want.Morning.GoldenHour},
		"evening golden": {r.Evening.GoldenHour, want.Evening.GoldenHour}, "evening blue": {r.Evening.BlueHour, want.Evening.BlueHour},
	}
	for name, s := range spans {
		if len(s[0]) != 2 || len(s[1]) != 2 {
			t.Fatalf("%s: %v, want %v", name, s[0], s[1])
		}
		pairs[name+" from"] = [2]string{s[0][0], s[1][0]}
		pairs[name+" to"] = [2]string{s[0][1], s[1][1]}
	}
	for name, p := range pairs {
		if d := minutes(t, p[0]) - minutes(t, p[1]); d < -1 || d > 1 {
			t.Errorf("%s %s, the frontend %s", name, p[0], p[1])
		}
	}
	if math.Abs(r.NoonElevationDeg-data.NoonElevationDeg) > 0.1 {
		t.Errorf("noon elevation %.2f, the frontend %.2f", r.NoonElevationDeg, data.NoonElevationDeg)
	}
}

func TestSunReportAtThePlaceAndTime(t *testing.T) {
	// Midsummer noon in Shanghai: high in the south, which with north at −Z is +Z, scene azimuth 0.
	r := sunReport(physical(map[string]any{"date": "2026-06-21", "local_time": "12:00"}))
	if r.ElevationDeg < 80 || math.Abs(r.CompassDeg-180) > 25 || !(r.SceneAzimuthDeg < 25 || r.SceneAzimuthDeg > 335) {
		t.Fatalf("Shanghai noon %+v", r)
	}
	// North turned to +X: the southern sun is at −X, scene azimuth 270.
	turned := sunReport(physical(map[string]any{"date": "2026-06-21", "local_time": "12:00", "north_azimuth_deg": 90}))
	if d := math.Mod(turned.SceneAzimuthDeg-r.SceneAzimuthDeg+720, 360); math.Abs(d-270) > 0.2 {
		t.Fatalf("north turned by −90°: scene azimuth %.1f from %.1f", turned.SceneAzimuthDeg, r.SceneAzimuthDeg)
	}
	// Noon in Sydney: in the northern sky.
	sydney := sunReport(physical(map[string]any{"date": "2026-06-21", "local_time": "12:00", "latitude_deg": -33.8688, "longitude_deg": 151.2093, "timezone": "Australia/Sydney"}))
	if !(sydney.CompassDeg < 15 || sydney.CompassDeg > 345) || sydney.ElevationDeg < 25 {
		t.Fatalf("Sydney noon %+v", sydney)
	}
}

func TestSunReportAboveTheArcticCircle(t *testing.T) {
	tromso := map[string]any{"latitude_deg": 69.6496, "longitude_deg": 18.956, "timezone": "Europe/Oslo"}
	with := func(date string) Sky {
		f := map[string]any{"date": date}
		for k, v := range tromso {
			f[k] = v
		}
		return physical(f)
	}
	summer := sunReport(with("2026-06-21"))
	if summer.Polar != "day" || summer.Sunrise != "" || summer.Sunset != "" || summer.Evening.GoldenHour != nil {
		t.Fatalf("midsummer %+v", summer)
	}
	winter := sunReport(with("2026-12-21"))
	if winter.Polar != "night" || winter.Sunrise != "" || winter.Morning.GoldenHour != nil {
		t.Fatalf("midwinter %+v", winter)
	}
}

// Only a physical sky placed by location has a sun reference.
func TestSunReportOnlyForALocatedPhysicalSky(t *testing.T) {
	elevation, azimuth, turbidity := 35.0, 135.0, 3.0
	if r := sunReport(Sky{Kind: "procedural", SunElevationDeg: &elevation, SunAzimuthDeg: &azimuth, Turbidity: &turbidity}); r != nil {
		t.Fatalf("procedural: %+v", r)
	}
	if r := sunReport(physical(map[string]any{"date": "2026-06-21", "sun_mode": "manual"})); r != nil {
		t.Fatalf("manual: %+v", r)
	}
}
