package creativescene

import (
	"math"
	"time"
)

// The moon reference (W3, Epic revision v4): what the assistant cannot see,
// as the sun reference (sun.go). It comes with a read of the scene and with
// an edit that wrote the environment, is never stored, and never
// fingerprints. The moon's place is Meeus's chapter 47 (Astronomical
// Algorithms, 2nd ed.; the series' coefficients as transcribed in
// github.com/soniakeys/meeus, MIT, docs/third-party-notices.md), about 10"
// in longitude; the renderer's astronomy-engine agrees within a few minutes
// of time. UT stands in for dynamical time (ΔT, about a minute, moves the
// moon 0.01°).

// MoonReport is the moon of a physical sky placed by location: at its local
// time, and around its local day. Times are HH:MM, dates YYYY-MM-DD, in the
// sky's time zone.
type MoonReport struct {
	// Phase is new, waxing_crescent, first_quarter, waxing_gibbous, full,
	// waning_gibbous, last_quarter or waning_crescent: the moon's ecliptic
	// longitude east of the sun in eighths centred on 0°, 90°, 180°, 270°.
	Phase string `json:"phase"`
	// Illumination is the illuminated fraction of the disc, 0…1.
	Illumination float64 `json:"illumination"`
	// ElevationDeg is the apparent elevation, parallax and refraction
	// included; CompassDeg is clockwise from north; SceneAzimuthDeg is the
	// document's azimuth (sun_azimuth_deg's convention, north counted in).
	ElevationDeg     float64 `json:"elevation_deg"`
	CompassDeg       float64 `json:"compass_deg"`
	SceneAzimuthDeg  float64 `json:"scene_azimuth_deg"`
	Moonrise         string  `json:"moonrise,omitempty"`
	Moonset          string  `json:"moonset,omitempty"`
	PreviousFullMoon string  `json:"previous_full_moon"`
	NextFullMoon     string  `json:"next_full_moon"`
	PreviousNewMoon  string  `json:"previous_new_moon"`
	NextNewMoon      string  `json:"next_new_moon"`
}

const (
	earthRadiusKM = 6378.14
	moonRadiusKM  = 1737.4
	sunDistanceKM = 149_597_870.7
	synodicDays   = 29.530589
)

var moonPhases = [...]string{"new", "waxing_crescent", "first_quarter", "waxing_gibbous", "full", "waning_gibbous", "last_quarter", "waning_crescent"}

// moonReport is the moon reference of sky, nil unless it is a physical sky
// whose sun (and so its moon) comes from its place and time. sky is complete
// and valid. The night sky's switches do not count: the reference says
// what is in the sky, not what is drawn.
func moonReport(sky Sky) *MoonReport {
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
	m := moonAt(now)
	h := m.horizontal(now, lat, lon)
	r := &MoonReport{
		Phase:           moonPhases[int(math.Mod(m.elongation+22.5, 360)/45)%8],
		Illumination:    math.Round(m.illumination*1000) / 1000,
		ElevationDeg:    round1(h.apparent),
		CompassDeg:      round1(h.compass),
		SceneAzimuthDeg: round1(documentAzimuth(h.compass, *sky.NorthAzimuthDeg)),
	}

	// Moonrise and moonset: the upper limb on the horizon, refraction
	// counted (the centre's geometric topocentric elevation at −34′ minus
	// the semidiameter), minute by minute and interpolated.
	n := int(end.Sub(start) / time.Minute)
	below := make([]float64, n+1)
	for i := range below {
		t := start.Add(time.Duration(i) * time.Minute)
		mi := moonAt(t)
		below[i] = mi.horizontal(t, lat, lon).geometric + 34.0/60 + rad2deg(math.Asin(moonRadiusKM/mi.distanceKM))
	}
	clock := func(minute float64) string {
		return start.Add(time.Duration(minute * float64(time.Minute))).In(loc).Round(time.Minute).Format("15:04")
	}
	for i := 1; i <= n; i++ {
		a, b := below[i-1], below[i]
		switch {
		case r.Moonrise == "" && a < 0 && b >= 0:
			r.Moonrise = clock(float64(i-1) + -a/(b-a))
		case r.Moonset == "" && a >= 0 && b < 0:
			r.Moonset = clock(float64(i-1) + a/(a-b))
		}
	}

	day := func(t time.Time) string { return t.In(loc).Format(time.DateOnly) }
	prev, next := phaseAround(start, 180)
	r.PreviousFullMoon, r.NextFullMoon = day(prev), day(next)
	prev, next = phaseAround(start, 0)
	r.PreviousNewMoon, r.NextNewMoon = day(prev), day(next)
	return r
}

// phaseAround is the last instant before start and the first at or after it
// at which the moon's elongation (ecliptic longitude east of the sun) is
// target: stepped every six hours, bisected to a second.
func phaseAround(start time.Time, target float64) (before, after time.Time) {
	// past is how far the elongation is past target, in −180…180.
	past := func(t time.Time) float64 { return math.Mod(math.Mod(moonAt(t).elongation-target, 360)+540, 360) - 180 }
	crossing := func(a, b time.Time) time.Time {
		for b.Sub(a) > time.Second {
			mid := a.Add(b.Sub(a) / 2)
			if past(mid) < 0 {
				a = mid
			} else {
				b = mid
			}
		}
		return b
	}
	const step = 6 * time.Hour
	from := start.Add(-time.Duration((synodicDays + 2) * 24 * float64(time.Hour)))
	for t := from; ; t = t.Add(step) {
		a, b := past(t), past(t.Add(step))
		if a < 0 && b >= 0 && b-a < 90 {
			at := crossing(t, t.Add(step))
			if !at.Before(start) {
				return before, at
			}
			before = at
		}
	}
}

// moonPlace is the moon's geocentric place at an instant: ecliptic of date
// (nutation in longitude added), distance, and its elongation and
// illuminated fraction against the sun.
type moonPlace struct {
	longitude, latitude, distanceKM float64
	elongation, illumination        float64
	jd                              float64
}

func moonAt(at time.Time) moonPlace {
	jd := float64(at.UnixMilli())/86_400_000 + 2440587.5
	t := (jd - 2451545.0) / 36525.0
	lambda, beta, delta := moonEcliptic(t)
	omega := deg2rad(125.04452 - 1934.136261*t)
	lambda += -0.004778 * math.Sin(omega)
	sun := apparentLongitude(t)
	// Meeus 48.2–48.3: the elongation ψ, then the phase angle i.
	lr, br, sr := deg2rad(lambda), deg2rad(beta), deg2rad(sun)
	psi := math.Acos(math.Cos(br) * math.Cos(lr-sr))
	i := math.Atan2(sunDistanceKM*math.Sin(psi), delta-sunDistanceKM*math.Cos(psi))
	return moonPlace{
		longitude: lambda, latitude: beta, distanceKM: delta, jd: jd,
		elongation:   math.Mod(math.Mod(lambda-sun, 360)+360, 360),
		illumination: (1 + math.Cos(i)) / 2,
	}
}

// moonHorizontal is where the moon is from a place at sea level: compass
// azimuth, and the topocentric elevation with and without refraction.
type moonHorizontal struct{ compass, apparent, geometric float64 }

func (m moonPlace) horizontal(at time.Time, latitudeDeg, longitudeDeg float64) moonHorizontal {
	t := (m.jd - 2451545.0) / 36525.0
	eps := deg2rad(obliquity(t))
	lr, br := deg2rad(m.longitude), deg2rad(m.latitude)
	ra := math.Atan2(math.Sin(lr)*math.Cos(eps)-math.Tan(br)*math.Sin(eps), math.Cos(lr))
	dec := math.Asin(math.Sin(br)*math.Cos(eps) + math.Cos(br)*math.Sin(eps)*math.Sin(lr))
	// Meeus 12.4: mean sidereal time at Greenwich (the equation of the equinoxes, under a second, left out).
	d := m.jd - 2451545.0
	gmst := 280.46061837 + 360.98564736629*d + 0.000387933*t*t - t*t*t/38710000
	hour := deg2rad(gmst+longitudeDeg) - ra
	lat := deg2rad(latitudeDeg)
	sinH := math.Sin(lat)*math.Sin(dec) + math.Cos(lat)*math.Cos(dec)*math.Cos(hour)
	h := math.Asin(math.Max(-1, math.Min(1, sinH)))
	// Azimuth from north, clockwise.
	az := math.Atan2(-math.Cos(dec)*math.Sin(hour), math.Sin(dec)*math.Cos(lat)-math.Cos(dec)*math.Sin(lat)*math.Cos(hour))
	// The observer is an Earth radius off the centre: the moon sits lower by its parallax.
	parallax := math.Asin(earthRadiusKM / m.distanceKM)
	geometric := rad2deg(h - parallax*math.Cos(h))
	return moonHorizontal{compass: math.Mod(rad2deg(az)+360, 360), geometric: geometric, apparent: geometric + refraction(geometric)}
}

// moonEcliptic is Meeus 47: the moon's geocentric longitude and latitude
// (mean equinox of date, degrees) and distance (km) at t Julian centuries
// since J2000.
func moonEcliptic(t float64) (lambda, beta, delta float64) {
	const p = math.Pi / 180
	horner := func(x float64, c ...float64) float64 {
		v := 0.0
		for i := len(c) - 1; i >= 0; i-- {
			v = v*x + c[i]
		}
		return v
	}
	lp := horner(t, 218.3164477*p, 481267.88123421*p, -.0015786*p, p/538841, -p/65194000)
	d := horner(t, 297.8501921*p, 445267.1114034*p, -.0018819*p, p/545868, -p/113065000)
	m := horner(t, 357.5291092*p, 35999.0502909*p, -.0001535*p, p/24490000)
	mp := horner(t, 134.9633964*p, 477198.8675055*p, .0087414*p, p/69699, -p/14712000)
	f := horner(t, 93.272095*p, 483202.0175233*p, -.0036539*p, -p/3526000, p/863310000)
	a1 := 119.75*p + 131.849*p*t
	a2 := 53.09*p + 479264.29*p*t
	a3 := 313.45*p + 481266.484*p*t
	e := horner(t, 1, -.002516, -.0000074)
	ecc := func(mCoef float64) float64 {
		switch math.Abs(mCoef) {
		case 1:
			return e
		case 2:
			return e * e
		}
		return 1
	}
	sl := 3958*math.Sin(a1) + 1962*math.Sin(lp-f) + 318*math.Sin(a2)
	sr := 0.0
	sb := -2235*math.Sin(lp) + 382*math.Sin(a3) + 175*math.Sin(a1-f) + 175*math.Sin(a1+f) + 127*math.Sin(lp-mp) - 115*math.Sin(lp+mp)
	for _, r := range moonLR {
		arg := d*r[0] + m*r[1] + mp*r[2] + f*r[3]
		sl += r[4] * math.Sin(arg) * ecc(r[1])
		sr += r[5] * math.Cos(arg) * ecc(r[1])
	}
	for _, r := range moonB {
		sb += r[4] * math.Sin(d*r[0]+m*r[1]+mp*r[2]+f*r[3]) * ecc(r[1])
	}
	lambda = math.Mod(math.Mod(lp/p, 360)+360, 360) + sl*1e-6
	return lambda, sb * 1e-6, 385000.56 + sr*1e-3
}

// moonLR is Meeus table 47.A: multiples of D, M, M′, F, then the
// coefficients of longitude (1e-6 °, sine) and distance (1e-3 km, cosine).
var moonLR = [...][6]float64{
	{0, 0, 1, 0, 6288774, -20905355},
	{2, 0, -1, 0, 1274027, -3699111},
	{2, 0, 0, 0, 658314, -2955968},
	{0, 0, 2, 0, 213618, -569925},
	{0, 1, 0, 0, -185116, 48888},
	{0, 0, 0, 2, -114332, -3149},
	{2, 0, -2, 0, 58793, 246158},
	{2, -1, -1, 0, 57066, -152138},
	{2, 0, 1, 0, 53322, -170733},
	{2, -1, 0, 0, 45758, -204586},
	{0, 1, -1, 0, -40923, -129620},
	{1, 0, 0, 0, -34720, 108743},
	{0, 1, 1, 0, -30383, 104755},
	{2, 0, 0, -2, 15327, 10321},
	{0, 0, 1, 2, -12528, 0},
	{0, 0, 1, -2, 10980, 79661},
	{4, 0, -1, 0, 10675, -34782},
	{0, 0, 3, 0, 10034, -23210},
	{4, 0, -2, 0, 8548, -21636},
	{2, 1, -1, 0, -7888, 24208},
	{2, 1, 0, 0, -6766, 30824},
	{1, 0, -1, 0, -5163, -8379},
	{1, 1, 0, 0, 4987, -16675},
	{2, -1, 1, 0, 4036, -12831},
	{2, 0, 2, 0, 3994, -10445},
	{4, 0, 0, 0, 3861, -11650},
	{2, 0, -3, 0, 3665, 14403},
	{0, 1, -2, 0, -2689, -7003},
	{2, 0, -1, 2, -2602, 0},
	{2, -1, -2, 0, 2390, 10056},
	{1, 0, 1, 0, -2348, 6322},
	{2, -2, 0, 0, 2236, -9884},
	{0, 1, 2, 0, -2120, 5751},
	{0, 2, 0, 0, -2069, 0},
	{2, -2, -1, 0, 2048, -4950},
	{2, 0, 1, -2, -1773, 4130},
	{2, 0, 0, 2, -1595, 0},
	{4, -1, -1, 0, 1215, -3958},
	{0, 0, 2, 2, -1110, 0},
	{3, 0, -1, 0, -892, 3258},
	{2, 1, 1, 0, -810, 2616},
	{4, -1, -2, 0, 759, -1897},
	{0, 2, -1, 0, -713, -2117},
	{2, 2, -1, 0, -700, 2354},
	{2, 1, -2, 0, 691, 0},
	{2, -1, 0, -2, 596, 0},
	{4, 0, 1, 0, 549, -1423},
	{0, 0, 4, 0, 537, -1117},
	{4, -1, 0, 0, 520, -1571},
	{1, 0, -2, 0, -487, -1739},
	{2, 1, 0, -2, -399, 0},
	{0, 0, 2, -2, -381, -4421},
	{1, 1, 1, 0, 351, 0},
	{3, 0, -2, 0, -340, 0},
	{4, 0, -3, 0, 330, 0},
	{2, -1, 2, 0, 327, 0},
	{0, 2, 1, 0, -323, 1165},
	{1, 1, -1, 0, 299, 0},
	{2, 0, 3, 0, 294, 0},
	{2, 0, -1, -2, 0, 8752},
}

// moonB is Meeus table 47.B: multiples of D, M, M′, F, then the coefficient
// of latitude (1e-6 °, sine).
var moonB = [...][5]float64{
	{0, 0, 0, 1, 5128122},
	{0, 0, 1, 1, 280602},
	{0, 0, 1, -1, 277693},
	{2, 0, 0, -1, 173237},
	{2, 0, -1, 1, 55413},
	{2, 0, -1, -1, 46271},
	{2, 0, 0, 1, 32573},
	{0, 0, 2, 1, 17198},
	{2, 0, 1, -1, 9266},
	{0, 0, 2, -1, 8822},
	{2, -1, 0, -1, 8216},
	{2, 0, -2, -1, 4324},
	{2, 0, 1, 1, 4200},
	{2, 1, 0, -1, -3359},
	{2, -1, -1, 1, 2463},
	{2, -1, 0, 1, 2211},
	{2, -1, -1, -1, 2065},
	{0, 1, -1, -1, -1870},
	{4, 0, -1, -1, 1828},
	{0, 1, 0, 1, -1794},
	{0, 0, 0, 3, -1749},
	{0, 1, -1, 1, -1565},
	{1, 0, 0, 1, -1491},
	{0, 1, 1, 1, -1475},
	{0, 1, 1, -1, -1410},
	{0, 1, 0, -1, -1344},
	{1, 0, 0, -1, -1335},
	{0, 0, 3, 1, 1107},
	{4, 0, 0, -1, 1021},
	{4, 0, -1, 1, 833},
	{0, 0, 1, -3, 777},
	{4, 0, -2, 1, 671},
	{2, 0, 0, -3, 607},
	{2, 0, 2, -1, 596},
	{2, -1, 1, -1, 491},
	{2, 0, -2, 1, -451},
	{0, 0, 3, -1, 439},
	{2, 0, 2, 1, 422},
	{2, 0, -3, -1, 421},
	{2, 1, -1, 1, -366},
	{2, 1, 0, 1, -351},
	{4, 0, 0, 1, 331},
	{2, -1, 1, 1, 315},
	{2, -2, 0, -1, 302},
	{0, 0, 1, 3, -283},
	{2, 1, 1, -1, -229},
	{1, 1, 0, -1, 223},
	{1, 1, 0, 1, 223},
	{0, 1, -2, -1, -220},
	{2, 1, -1, -1, -220},
	{1, 0, 1, 1, -185},
	{2, -1, -2, -1, 181},
	{0, 1, 2, 1, -177},
	{4, 0, -2, -1, 176},
	{4, -1, -1, -1, 166},
	{1, 0, 1, -1, -164},
	{4, 0, 1, -1, 132},
	{1, 0, -1, -1, -119},
	{4, -1, 0, -1, 115},
	{2, -2, 0, 1, 107},
}
