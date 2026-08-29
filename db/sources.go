package db

// Booking sources: where the work came from. Public bookings get one detected
// from the request (see the attribution middleware); the admin picks one by
// hand when taking a booking over the phone. Rows created before this column
// existed carry "", which reads as "Unknown".
const (
	SourceGoogleAds     = "google-ads"
	SourceGoogleSearch  = "google-search"
	SourceGoogleProfile = "google-profile"
	SourcePhone         = "phone"
	SourceReferral      = "referral"
	SourceRepeat        = "repeat"
	SourceCard          = "card"
	SourceDirect        = "direct"
	SourceOther         = "other"
)

// BookingSource pairs a stored value with the label shown in the admin.
type BookingSource struct {
	Value, Label string
}

// BookingSources are the options offered when the admin records a booking by
// hand, in the order they appear in the dropdown. Phone leads because that's
// what the form is for.
var BookingSources = []BookingSource{
	{SourcePhone, "Phone call"},
	{SourceGoogleAds, "Google Ads"},
	{SourceGoogleSearch, "Google search"},
	{SourceGoogleProfile, "Google Business Profile"},
	{SourceReferral, "Word of mouth"},
	{SourceRepeat, "Repeat customer"},
	{SourceCard, "Card or flyer"},
	{SourceDirect, "Website"},
	{SourceOther, "Other"},
}

// SourceLabel renders a stored source for display. Unrecognised values are
// returned as-is so a referrer host like "bing.com" still reads sensibly.
func SourceLabel(v string) string {
	if v == "" {
		return "Unknown"
	}
	for _, s := range BookingSources {
		if s.Value == v {
			return s.Label
		}
	}
	return v
}

// ValidSource reports whether v is one of the admin-selectable sources.
func ValidSource(v string) bool {
	for _, s := range BookingSources {
		if s.Value == v {
			return true
		}
	}
	return false
}
