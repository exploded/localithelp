package main

import (
	"sort"
	"strings"
)

// Suburb is one entry in the published service area. Each gets a local landing
// page at /areas/{slug}; the copy is authored here so every page says something
// specific about the place rather than swapping the name into a template.
type Suburb struct {
	Name     string
	Slug     string
	Postcode string
	LGA      string // council area, used to group the index and pick "nearby" links
	DriveMin int    // rough drive time from Donvale, minutes
	Blurb    string // 2-3 sentences of local context shown on the page
	MetaDesc string // meta description (unique per suburb)
	Photo    *Photo // credit for static/img/areas/{slug}.jpg when it needs one (nil = own photo / no photo)
}

// Photo is the attribution for a suburb page image sourced under a free licence.
// Own photos need no Photo entry — just drop the file in (see tools/areaphoto).
type Photo struct {
	Artist     string // photographer / uploader as credited at the source
	Licence    string // "CC BY-SA 4.0", "Public domain", ...
	LicenceURL string // deed URL ("" for public domain)
	Source     string // where it came from (Commons file page)
}

// URL returns the canonical path for the suburb page.
func (s *Suburb) URL() string { return "/areas/" + s.Slug }

// suburbGroup is one council area on the /areas index.
type suburbGroup struct {
	LGA     string
	Suburbs []*Suburb
}

// lgaOrder controls the order of council groups on /areas (closest first).
var lgaOrder = []string{"Manningham", "Whitehorse", "Maroondah", "Boroondara", "Nillumbik", "Yarra Ranges", "Knox", "Monash"}

var (
	suburbs       []string // display names, in catalogue order (used by templates + JSON-LD areaServed)
	suburbsBySlug = map[string]*Suburb{}
)

func init() {
	for i := range suburbList {
		suburbsBySlug[suburbList[i].Slug] = &suburbList[i]
		suburbs = append(suburbs, suburbList[i].Name)
	}
}

// retiredSuburbs are slugs that once had a page but fell outside the service
// area when it was tightened on 2026-08-23. They 301 to /areas so the indexed
// URLs don't become 404s. Keep until Search Console shows them dropped.
var retiredSuburbs = map[string]bool{
	"camberwell": true, "canterbury": true, "chirnside-park": true,
	"eaglemont": true, "eltham": true, "eltham-north": true,
	"greensborough": true, "heidelberg": true, "knoxfield": true,
	"lilydale": true, "lower-plenty": true, "montmorency": true,
	"rosanna": true, "viewbank": true, "watsonia": true, "wheelers-hill": true,
}

func findSuburb(slug string) (*Suburb, bool) {
	s, ok := suburbsBySlug[slug]
	return s, ok
}

// findSuburbByName matches a display name case-insensitively (used to validate ?suburb= prefills).
func findSuburbByName(name string) (*Suburb, bool) {
	name = strings.TrimSpace(name)
	for i := range suburbList {
		if strings.EqualFold(suburbList[i].Name, name) {
			return &suburbList[i], true
		}
	}
	return nil, false
}

// Nearby returns up to six other suburbs, same council first, then by drive time.
func (s *Suburb) Nearby() []*Suburb {
	var out []*Suburb
	for i := range suburbList {
		if suburbList[i].Slug != s.Slug {
			out = append(out, &suburbList[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].LGA == s.LGA, out[j].LGA == s.LGA
		if ai != aj {
			return ai
		}
		return absInt(out[i].DriveMin-s.DriveMin) < absInt(out[j].DriveMin-s.DriveMin)
	})
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// groupSuburbs buckets the catalogue by council area for the /areas index.
func groupSuburbs() []suburbGroup {
	byLGA := map[string]*suburbGroup{}
	var groups []*suburbGroup
	for _, l := range lgaOrder {
		g := &suburbGroup{LGA: l}
		byLGA[l] = g
		groups = append(groups, g)
	}
	for i := range suburbList {
		g, ok := byLGA[suburbList[i].LGA]
		if !ok {
			g = &suburbGroup{LGA: suburbList[i].LGA}
			byLGA[suburbList[i].LGA] = g
			groups = append(groups, g)
		}
		g.Suburbs = append(g.Suburbs, &suburbList[i])
	}
	var out []suburbGroup
	for _, g := range groups {
		if len(g.Suburbs) > 0 {
			out = append(out, *g)
		}
	}
	return out
}

// suburbList is the published service area (roughly 30 minutes' drive from Donvale).
// Postcodes and councils are authored by hand — keep them accurate.
var suburbList = []Suburb{
	// ── Manningham ──
	{Name: "Donvale", Slug: "donvale", Postcode: "3111", LGA: "Manningham", DriveMin: 5,
		Blurb:    "Donvale is home base, so this is the quickest visit on the list — the leafy blocks between Mitcham Road and Springvale Road, out towards Tunstall Square and Mullum Mullum Creek. Big established gardens often mean Wi-Fi that reaches the kitchen but not the back study, and plenty of longer-held desktops that just need a tune-up rather than a replacement.",
		MetaDesc: "At-home computer help in Donvale 3111 — Wi-Fi that won't reach, slow PCs, email, printers and scam clean-ups, usually same or next day. No fix, no fee.",
		Photo:    &Photo{Artist: "Nick carson at English Wikipedia", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Mullum_mullum_creek_linear_park_path.JPG"}},
	{Name: "Doncaster", Slug: "doncaster", Postcode: "3108", LGA: "Manningham", DriveMin: 10,
		Blurb:    "Ten minutes down Doncaster Road, from the Westfield end through to the apartments around the hill. Doncaster has a real mix — new units where the NBN box is buried in a cupboard, and family homes where the whole household shares one ageing laptop. Setting up a new computer and moving everything across is one of the most common jobs here.",
		MetaDesc: "Computer help at your place in Doncaster 3108 — new-computer setups, NBN and Wi-Fi fixes, email, printers and virus removal. Local, friendly, no fix no fee.",
		Photo:    &Photo{Artist: "Chicken7", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Doncaster-front.jpg"}},
	{Name: "Doncaster East", Slug: "doncaster-east", Postcode: "3109", LGA: "Manningham", DriveMin: 7,
		Blurb:    "Just over the Donvale boundary — The Pines, Jackson Court, the streets off Blackburn Road. Doncaster East is close enough that same-day visits are the norm rather than the exception. Lots of retirees who'd like their email and photos sorted properly, and lots of home offices that need a printer that actually prints.",
		MetaDesc: "Same-day at-home computer help in Doncaster East 3109 — email, printers, Wi-Fi, slow computers, one-on-one help. Five minutes from Donvale; no fix, no fee.",
		Photo:    &Photo{Artist: "Bob Tan", Licence: "CC BY 4.0", LicenceURL: "https://creativecommons.org/licenses/by/4.0", Source: "https://commons.wikimedia.org/wiki/File:Aerial_panorama_of_Ruffey_Lake_Park._Sunset_24_September_2023.jpg"}},
	{Name: "Templestowe", Slug: "templestowe", Postcode: "3106", LGA: "Manningham", DriveMin: 10,
		Blurb:    "Big blocks, long driveways and houses that sprawl — which is exactly the recipe for Wi-Fi dead spots. Templestowe visits are very often about getting a strong signal to the far end of the house or the studio out the back, plus the usual email, printer and backup jobs along the way.",
		MetaDesc: "Computer and Wi-Fi help at home in Templestowe 3106 — mesh Wi-Fi for large homes, email, printers, backups and repairs. Local, plain-English, no fix no fee.",
		Photo:    &Photo{Artist: "Ottre", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Streetview_Templestowe.jpg"}},
	{Name: "Templestowe Lower", Slug: "templestowe-lower", Postcode: "3107", LGA: "Manningham", DriveMin: 12,
		Blurb:    "Down towards the Yarra flats and Macedon Square. Templestowe Lower has a lot of long-time residents who've had the same computer for years and would rather someone came to them than lug it to a shop. Slow-PC tune-ups, scam call clean-ups and helping with phones and tablets are the regular calls here.",
		MetaDesc: "At-home computer help in Templestowe Lower 3107 — slow computers, scam clean-ups, phones and tablets, email and printers. I come to you; no fix, no fee.",
		Photo:    &Photo{Artist: "Ottre", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Streetview_Lower_Templestowe.JPG"}},
	{Name: "Bulleen", Slug: "bulleen", Postcode: "3105", LGA: "Manningham", DriveMin: 15,
		Blurb:    "Bulleen sits at the city end of Manningham, along Thompsons Road and Bulleen Road. Plenty of family homes with a shared computer and a full complement of phones and tablets that all need to talk to the same printer and Wi-Fi. Small businesses along the shopping strips call for point-of-sale, email and backup help too.",
		MetaDesc: "Computer help at home or your shop in Bulleen 3105 — Wi-Fi, printers, email, backups and small-business IT. Friendly, local, no fix no fee.",
		Photo:    &Photo{Artist: "BobTanGo", Licence: "CC BY 4.0", LicenceURL: "https://creativecommons.org/licenses/by/4.0", Source: "https://commons.wikimedia.org/wiki/File:Yarra_Flats_Park_facing_the_Melbourne_skyline._April_2024.jpg"}},
	{Name: "Park Orchards", Slug: "park-orchards", Postcode: "3114", LGA: "Manningham", DriveMin: 8,
		Blurb:    "Acreage blocks and winding roads mean Park Orchards is a place where good internet is hard-won. I spend a lot of time here getting NBN, 4G/5G or Starlink connections working reliably and pushing Wi-Fi out to sheds and studios. Being next door to Donvale, it's usually a quick trip.",
		MetaDesc: "Internet, Wi-Fi and computer help in Park Orchards 3114 — NBN and mobile broadband setups, whole-property Wi-Fi, PCs and printers. Next door to Donvale.",
		Photo:    &Photo{Artist: "Kiewa", Licence: "CC BY-SA 3.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/3.0", Source: "https://commons.wikimedia.org/wiki/File:Park_Orchards_shops,_Park_Road,_Park_Orchards,_Australia.jpg"}},
	{Name: "Warrandyte", Slug: "warrandyte", Postcode: "3113", LGA: "Manningham", DriveMin: 12,
		Blurb:    "Bush blocks along the Yarra, houses tucked into the hills, and internet that's often less than ideal. Warrandyte jobs lean towards connectivity — mobile broadband, mesh Wi-Fi, getting a signal to the far end of the house — plus the usual email, printer and backup work for the many home-based businesses and creatives around the village.",
		MetaDesc: "Computer, internet and Wi-Fi help at home in Warrandyte 3113 — patchy connections fixed, mesh Wi-Fi, PCs, printers and backups. Local, no fix no fee.",
		Photo:    &Photo{Artist: "Nick carson at English Wikipedia", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Yarra_River_at_Warrandyte.jpg"}},
	{Name: "Wonga Park", Slug: "wonga-park", Postcode: "3115", LGA: "Manningham", DriveMin: 15,
		Blurb:    "Out past Warrandyte on the semi-rural fringe, Wonga Park has big properties, home offices and more than a few sheds with a computer in them. Reliable internet and Wi-Fi that reaches everywhere are the usual asks, along with data recovery and backups for people who've built up years of photos and records.",
		MetaDesc: "At-home computer help in Wonga Park 3115 — internet and Wi-Fi across large properties, backups, data recovery, PCs and printers. Friendly and local.",
		Photo:    &Photo{Artist: "Melburnian", Licence: "CC BY 2.5", LicenceURL: "https://creativecommons.org/licenses/by/2.5", Source: "https://commons.wikimedia.org/wiki/File:Yarra_River_Wonga_Park.jpg"}},

	// ── Whitehorse ──
	{Name: "Mitcham", Slug: "mitcham", Postcode: "3132", LGA: "Whitehorse", DriveMin: 8,
		Blurb:    "Straight down Mitcham Road, so Mitcham is one of my closest suburbs. Around the station, Britannia Mall and the quiet streets either side of Whitehorse Road there's a real spread of households — young families with a houseful of devices, and long-time locals who'd like their computer to just behave. Both are very welcome.",
		MetaDesc: "Computer help at your place in Mitcham 3132 — email, Wi-Fi, printers, slow or broken PCs, scam clean-ups and patient one-on-one help. Minutes from Donvale.",
		Photo:    &Photo{Artist: "Melburnian", Licence: "CC BY 3.0", LicenceURL: "https://creativecommons.org/licenses/by/3.0", Source: "https://commons.wikimedia.org/wiki/File:Mitcham_Post_Office.jpg"}},
	{Name: "Nunawading", Slug: "nunawading", Postcode: "3131", LGA: "Whitehorse", DriveMin: 10,
		Blurb:    "Nunawading runs along Whitehorse Road and Springvale Road, with plenty of homes, units and small businesses in between. I do a lot of home-office and small-business work here — printers, email on every device, backups that actually run — as well as the everyday household jobs.",
		MetaDesc: "Home and small-business computer help in Nunawading 3131 — printers, email, Wi-Fi, backups, repairs and setups. Local, at your place, no fix no fee.",
		Photo:    &Photo{Artist: "Ottre", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Intersection_Nunawading1.jpg"}},
	{Name: "Vermont", Slug: "vermont", Postcode: "3133", LGA: "Whitehorse", DriveMin: 12,
		Blurb:    "Quiet, established and very much a family suburb around Canterbury Road and Boronia Road. Vermont jobs are often about the household basics done properly — a new laptop set up with everything moved across, a printer that works from every phone, and a Wi-Fi network that covers the whole house.",
		MetaDesc: "At-home computer help in Vermont 3133 — new laptop setups, printers, whole-house Wi-Fi, email and repairs. Friendly, local, no fix no fee."},
	{Name: "Vermont South", Slug: "vermont-south", Postcode: "3133", LGA: "Whitehorse", DriveMin: 15,
		Blurb:    "Around the Vermont South shopping centre and the end of the tram line. A good number of retirees and semi-retirees here who want unhurried, plain-English help — sorting email, getting photos off the phone, dealing with a scam call — and I'm happy to sit down and go through it at your own pace.",
		MetaDesc: "Patient at-home computer help in Vermont South 3133 — email, photos, phones and tablets, scam clean-ups and one-on-one tuition. I come to you.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Morack_Road,_Vermont_South.jpg"}},
	{Name: "Forest Hill", Slug: "forest-hill", Postcode: "3131", LGA: "Whitehorse", DriveMin: 12,
		Blurb:    "Between Canterbury Road and Burwood Highway, around Forest Hill Chase. Homes here often have a family computer that's a few years old and slowing down, plus a printer everyone fights with. A tune-up or a clean Windows reinstall usually buys a few more good years, and I'll tell you honestly if it doesn't make sense.",
		MetaDesc: "Computer help at home in Forest Hill 3131 — slow PCs sped up, printers fixed, Wi-Fi, email and honest advice on repair vs replace. No fix, no fee.",
		Photo:    &Photo{Artist: "Lakeyboy", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Forest_Hill_Chase_3_Level_View.JPG"}},
	{Name: "Blackburn", Slug: "blackburn", Postcode: "3130", LGA: "Whitehorse", DriveMin: 12,
		Blurb:    "Blackburn's leafy streets around the lake and the village have a lot of home offices and a lot of people who work from home a couple of days a week. Getting a reliable, secure setup — Wi-Fi that holds up on video calls, backups, a printer that just works — is most of what I do here.",
		MetaDesc: "Home-office and household computer help in Blackburn 3130 — Wi-Fi for video calls, backups, printers, email and repairs. Local, at your place.",
		Photo:    &Photo{Artist: "Wong_bejo (talk) (Uploads)", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:BlackburnLakeVicAU.JPG"}},
	{Name: "Box Hill", Slug: "box-hill", Postcode: "3128", LGA: "Whitehorse", DriveMin: 15,
		Blurb:    "Apartments around Box Hill Central, older homes in the surrounding streets, and lots of small businesses. In the units it's usually about NBN and Wi-Fi in a tricky spot; in the shops it's point-of-sale, email and getting the printer to talk to everything. I'm equally happy to come to your flat, house or counter.",
		MetaDesc: "Computer help in Box Hill 3128 — apartment Wi-Fi and NBN, small-business IT, printers, email and repairs at your home or shop. No fix, no fee.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Aerial_view_of_Box_Hill_CBD_looking_east_from_Kingsley_Gardens_in_Mont_Albert,_Melbourne.jpg"}},
	{Name: "Box Hill North", Slug: "box-hill-north", Postcode: "3129", LGA: "Whitehorse", DriveMin: 12,
		Blurb:    "Just up from Box Hill, between Elgar Road and Middleborough Road. Box Hill North has a lot of long-established households and a good share of multi-generational homes, so I'm often setting things up so grandparents, parents and kids can all use the same Wi-Fi, printer and shared photos without getting in each other's way.",
		MetaDesc: "At-home computer help in Box Hill North 3129 — family Wi-Fi and printers, new-computer setups, email, phones and tablets. Local, plain-English, no fix no fee.",
		Photo:    &Photo{Artist: "Philip Mallis from Melbourne", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Bushy_Creek_Trail,_Mont_Albert_North_(30560247513).jpg"}},
	{Name: "Blackburn North", Slug: "blackburn-north", Postcode: "3130", LGA: "Whitehorse", DriveMin: 12,
		Blurb:    "North of the railway line, between Springfield Road and Middleborough Road, Blackburn North is a mix of original post-war homes and newer townhouses. The older houses tend to have solid brick walls that Wi-Fi struggles to get through, so mesh setups and getting a decent signal to the back rooms come up often, alongside the usual email, printer and new-computer jobs.",
		MetaDesc: "At-home computer help in Blackburn North 3130 — Wi-Fi that reaches every room, email, printers, slow PCs and new-computer setups. Local; no fix, no fee."},
	{Name: "Blackburn South", Slug: "blackburn-south", Postcode: "3130", LGA: "Whitehorse", DriveMin: 15,
		Blurb:    "Down towards Canterbury Road and the shops on Blackburn Road. Blackburn South has a lot of long-settled households where one desktop does everything and nobody's quite sure where the backups are. Tune-ups, backup setups and patient one-on-one sessions are the regular calls, plus printers that stopped talking to the laptop after an update.",
		MetaDesc: "Computer help at your place in Blackburn South 3130 — slow computers, backups, printers, email and one-on-one help. Friendly and local; no fix, no fee."},
	{Name: "Box Hill South", Slug: "box-hill-south", Postcode: "3128", LGA: "Whitehorse", DriveMin: 15,
		Blurb:    "The quieter side of Box Hill, between Canterbury Road and Surrey Park. Plenty of family homes here juggling schoolwork, streaming and a home office on the same connection, which is usually where the trouble starts. Wi-Fi that drops out, printers nobody can find on the network, and setting up new laptops properly are the common jobs.",
		MetaDesc: "At-home computer help in Box Hill South 3128 — Wi-Fi drop-outs, printers, new laptop setups, email and virus clean-ups. I come to you; no fix, no fee."},
	{Name: "Burwood East", Slug: "burwood-east", Postcode: "3151", LGA: "Whitehorse", DriveMin: 15,
		Blurb:    "Along the Burwood Highway around Tally Ho and the Deakin end. Burwood East mixes established family homes with a good number of students and home offices, so it's a lot of laptop trouble, shared Wi-Fi and getting printers and cloud storage working across several devices. Small businesses along the highway call for email and backup help too.",
		MetaDesc: "Computer help at home or work in Burwood East 3151 — laptops, Wi-Fi, printers, cloud storage and small-business IT. Local, plain-English, no fix no fee."},
	{Name: "Mont Albert North", Slug: "mont-albert-north", Postcode: "3129", LGA: "Whitehorse", DriveMin: 15,
		Blurb:    "The pocket between Belmore Road and the Koonung Creek reserve, north of Mont Albert proper. It's a settled, quiet area with a lot of older residents who'd much rather someone came to the house than carried a computer into a shop. Scam and virus clean-ups, email problems and patient sit-down sessions are the usual reasons for a visit.",
		MetaDesc: "At-home computer help in Mont Albert North 3129 — scam clean-ups, email, printers, slow computers and patient one-on-one help. No fix, no fee."},

	// ── Maroondah ──
	{Name: "Ringwood", Slug: "ringwood", Postcode: "3134", LGA: "Maroondah", DriveMin: 12,
		Blurb:    "Ringwood is a quick run out along Maroondah Highway — Eastland, the station precinct and the residential streets around Ringwood Lake. There's a bit of everything here: home users, apartments, and small businesses that need reliable email, backups and a printer that works from every device.",
		MetaDesc: "Computer help at your place in Ringwood 3134 — printers, Wi-Fi, email, backups, virus and scam clean-ups, PC repairs. Local, same or next day where possible.",
		Photo:    &Photo{Artist: "Unknown", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Ringwood_Cloclocktower.JPG"}},
	{Name: "Ringwood North", Slug: "ringwood-north", Postcode: "3134", LGA: "Maroondah", DriveMin: 10,
		Blurb:    "Bordering Warrandyte and Park Orchards, Ringwood North has larger homes and plenty of gardens between you and the modem. Whole-house Wi-Fi is the classic job, along with new-computer setups and getting older machines running smoothly again. Being so close to Donvale, it's usually an easy same-day visit.",
		MetaDesc: "At-home Wi-Fi and computer help in Ringwood North 3134 — whole-house Wi-Fi, PC tune-ups, setups, email and printers. Close to Donvale; no fix, no fee.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Maroondah_Highway,_North_Ringwood.jpg"}},
	{Name: "Ringwood East", Slug: "ringwood-east", Postcode: "3135", LGA: "Maroondah", DriveMin: 15,
		Blurb:    "Around the Ringwood East shops and station, out towards Croydon. A friendly, settled suburb where a lot of people would rather have someone come to the house than deal with a phone queue. Slow computers, scam calls, printer trouble and getting a new phone set up are the everyday requests here.",
		MetaDesc: "Computer help at home in Ringwood East 3135 — slow PCs, scam clean-ups, printers, phones and tablets, patient one-on-one help. No fix, no fee.",
		Photo:    &Photo{Artist: "Space999", Licence: "CC BY-SA 4.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/4.0", Source: "https://commons.wikimedia.org/wiki/File:RingwoodEastNewTrainStation.jpg"}},
	{Name: "Heathmont", Slug: "heathmont", Postcode: "3135", LGA: "Maroondah", DriveMin: 15,
		Blurb:    "Heathmont's hilly, tree-lined streets around the village and Dandenong Creek make for lovely walks and awkward Wi-Fi. I spend a fair bit of time here sorting out signal across split-level homes, plus the usual email, backup and new-computer jobs for households and home offices.",
		MetaDesc: "At-home computer and Wi-Fi help in Heathmont 3135 — split-level Wi-Fi fixed, email, backups, new-computer setups and repairs. Local and friendly.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Canterbury_Road,_Heathmont.jpg"}},
	{Name: "Croydon", Slug: "croydon", Postcode: "3136", LGA: "Maroondah", DriveMin: 15,
		Blurb:    "Croydon has a proper town centre and a wide spread of homes from Main Street out towards the hills. I do a lot of everyday household work here — a computer that's slowed to a crawl, a printer that's gone offline, email that stopped syncing — and I'm happy to come out for the small jobs as well as the big ones.",
		MetaDesc: "Computer help at your place in Croydon 3136 — slow PCs, printers, email, Wi-Fi, scam and virus clean-ups, new setups. Local, no fix no fee.",
		Photo:    &Photo{Artist: "NatoV", Licence: "CC BY-SA 4.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/4.0", Source: "https://commons.wikimedia.org/wiki/File:Croydon_Victoria_Main_Street.jpg"}},
	{Name: "Warranwood", Slug: "warranwood", Postcode: "3134", LGA: "Maroondah", DriveMin: 12,
		Blurb:    "Between Wonga Road and the Warranwood Reserve, on larger blocks that back onto bushland. Big houses and long driveways mean Wi-Fi rarely reaches the whole property on its own, so mesh and outdoor access points come up a lot here — along with backups, new-computer setups and the occasional laptop that's stopped booting.",
		MetaDesc: "Computer and Wi-Fi help in Warranwood 3134 — whole-house mesh Wi-Fi, backups, new-computer setups and repairs. Close to Donvale; no fix, no fee."},
	{Name: "Croydon North", Slug: "croydon-north", Postcode: "3136", LGA: "Maroondah", DriveMin: 15,
		Blurb:    "Up around Exeter Road and Yarra Road, on the rising ground north of the Croydon shops. It's a quieter, established part of Croydon with plenty of families and retirees, and the jobs reflect that — printers, email, phones and tablets that won't sync, and getting a new computer set up with everything moved across from the old one.",
		MetaDesc: "At-home computer help in Croydon North 3136 — printers, email, phones and tablets, new-computer setups and repairs. Friendly and local; no fix, no fee."},
	{Name: "Croydon Hills", Slug: "croydon-hills", Postcode: "3136", LGA: "Maroondah", DriveMin: 15,
		Blurb:    "A newer estate on the hills north-west of Croydon, with wide streets and two-storey homes. Double-storey usually means the modem sits in one corner downstairs and the study upstairs gets nothing, so mesh Wi-Fi is a frequent call. Beyond that it's the usual mix — new laptops, printers, backups and clearing up scam pop-ups.",
		MetaDesc: "Computer and Wi-Fi help in Croydon Hills 3136 — mesh Wi-Fi for two-storey homes, laptops, printers, backups and scam clean-ups. No fix, no fee."},
	{Name: "Croydon South", Slug: "croydon-south", Postcode: "3136", LGA: "Maroondah", DriveMin: 18,
		Blurb:    "Down towards Bayswater Road and Eastfield Park. Croydon South is a practical, settled area with a lot of original homes and a good number of small businesses run from the shed or the spare room. Typical visits are slow computers, printers, backups and sorting out email that's stopped sending after a provider change.",
		MetaDesc: "At-home computer help in Croydon South 3136 — slow PCs, printers, email problems, backups and small-business IT. I come to you; no fix, no fee."},

	// ── Yarra Ranges ──
	{Name: "Kilsyth", Slug: "kilsyth", Postcode: "3137", LGA: "Yarra Ranges", DriveMin: 20,
		Blurb:    "Out along Colchester Road at the foot of the hills, Kilsyth is a mix of family homes, larger blocks and light industry. Home users here often want the basics sorted properly — Wi-Fi, email, printer, backup — and the small workshops and trades want a computer setup that keeps the invoicing and quoting running without drama.",
		MetaDesc: "Computer help at home or work in Kilsyth 3137 — Wi-Fi, email, printers, backups and small-business setups. Friendly, local, no fix no fee.",
		Photo:    &Photo{Artist: "Matthew Paul Argall", Licence: "CC BY 2.0", LicenceURL: "https://creativecommons.org/licenses/by/2.0", Source: "https://commons.wikimedia.org/wiki/File:Montgomery_Court,_Kilsyth.jpg"}},
	{Name: "Mooroolbark", Slug: "mooroolbark", Postcode: "3138", LGA: "Yarra Ranges", DriveMin: 20,
		Blurb:    "Mooroolbark's a big, family-oriented suburb around the station and Brice Avenue, with plenty of homes that have grown a tangle of devices over the years. Getting everything on the one Wi-Fi network, backing up the photos and giving the main computer a good clean-out are the regular jobs.",
		MetaDesc: "At-home computer help in Mooroolbark 3138 — Wi-Fi for every device, backups, PC clean-outs, email, printers and phone/tablet help. No fix, no fee.",
		Photo:    &Photo{Artist: "Phenix1888", Licence: "CC BY-SA 4.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/4.0", Source: "https://commons.wikimedia.org/wiki/File:Mooroolbark_station_front.jpg"}},

	// ── Boroondara ──
	{Name: "Surrey Hills", Slug: "surrey-hills", Postcode: "3127", LGA: "Boroondara", DriveMin: 18,
		Blurb:    "Period homes, deep blocks and thick brick walls — Surrey Hills is charming and hard on Wi-Fi. A lot of my visits here are about getting a decent signal through an old house, plus helping people who've had the same trusty setup for years and want it working properly, not replaced for the sake of it.",
		MetaDesc: "Computer and Wi-Fi help at home in Surrey Hills 3127 — Wi-Fi through old brick homes, PC tune-ups, email, printers and repairs. No fix, no fee.",
		Photo:    &Photo{Artist: "Peter Campbell", Licence: "CC BY-SA 3.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/3.0", Source: "https://commons.wikimedia.org/wiki/File:Surrey_Gardens_Surrey_Hills_Australia_2.JPG"}},
	{Name: "Balwyn", Slug: "balwyn", Postcode: "3103", LGA: "Boroondara", DriveMin: 18,
		Blurb:    "Along Whitehorse Road and the quiet streets either side. Balwyn homes are often large and well established, with home offices, a study for the kids and a good number of older residents who'd like a patient hand with email, banking safely and keeping the scammers out. All of which I'm very happy to help with.",
		MetaDesc: "At-home computer help in Balwyn 3103 — home offices, whole-house Wi-Fi, safe online banking, scam protection and patient one-on-one help. Local, no fix no fee.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Knutsford_Street,_Balwyn.jpg"}},
	{Name: "Balwyn North", Slug: "balwyn-north", Postcode: "3104", LGA: "Boroondara", DriveMin: 15,
		Blurb:    "Just across the river from Bulleen, Balwyn North is a quick trip via the Eastern Freeway. It's a family suburb with a lot of devices per household — I do plenty of Wi-Fi, printer and shared-photo setups here, and a steady stream of new-computer handovers where everything needs to move across cleanly.",
		MetaDesc: "Computer help at your place in Balwyn North 3104 — new-computer setups, Wi-Fi, printers, photo backups and repairs. Local, quick to reach, no fix no fee.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Dunstan_Street,_Balwyn_North.jpg"}},

	// ── Nillumbik ──
	{Name: "North Warrandyte", Slug: "north-warrandyte", Postcode: "3113", LGA: "Nillumbik", DriveMin: 15,
		Blurb:    "Across the bridge on the Nillumbik side, where the blocks are bush and the internet is often the weakest link. North Warrandyte visits lean heavily towards connectivity — mobile broadband, Starlink, mesh Wi-Fi out to studios and sheds — plus backups and data recovery for people with years of photos on a single ageing drive.",
		MetaDesc: "Internet, Wi-Fi and computer help in North Warrandyte 3113 — mobile broadband, Starlink, mesh Wi-Fi, backups and data recovery. Local; no fix, no fee."},

	// ── Knox ──
	{Name: "Bayswater", Slug: "bayswater", Postcode: "3153", LGA: "Knox", DriveMin: 20,
		Blurb:    "Bayswater mixes family homes with a big light-industrial area, so my visits here split between households and small workshops. For homes it's the usual Wi-Fi, printer and email work; for businesses it's keeping the quoting, invoicing and email running, with a backup in place for when a computer finally gives up.",
		MetaDesc: "Computer help for homes and small businesses in Bayswater 3153 — Wi-Fi, printers, email, backups, repairs and setups at your place. No fix, no fee.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Mountain_Highway,_Bayswater_(2).jpg"}},
	{Name: "Wantirna", Slug: "wantirna", Postcode: "3152", LGA: "Knox", DriveMin: 18,
		Blurb:    "Along Mountain Highway and Boronia Road, near Knox Private and the Westfield end. Wantirna's a big family suburb and most calls are household ones — a computer that's slowed right down, a printer that's dropped off the Wi-Fi, a new phone that needs everything moved across.",
		MetaDesc: "At-home computer help in Wantirna 3152 — slow PCs, printers, Wi-Fi, new phones and laptops set up, email and repairs. Friendly, local, no fix no fee.",
		Photo:    &Photo{Artist: "Andrew Lorimer", Licence: "CC BY-SA 4.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/4.0", Source: "https://commons.wikimedia.org/wiki/File:Koomba_Park_trail.jpg"}},
	{Name: "Boronia", Slug: "boronia", Postcode: "3155", LGA: "Knox", DriveMin: 25,
		Blurb:    "At the foot of the Dandenongs, Boronia has a busy centre and a wide spread of homes up towards the hills. I come out for the everyday jobs — email, printers, slow computers, scam clean-ups — and for helping people who'd rather learn to do things themselves with someone patient sitting next to them.",
		MetaDesc: "Computer help at your place in Boronia 3155 — email, printers, slow PCs, scam clean-ups and one-on-one tuition. Local, plain-English, no fix no fee.",
		Photo:    &Photo{Artist: "HelloMojo at en.wikipedia", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Boronia,_Vic,_east_towards_One_Tree_Hill.JPG"}},
	{Name: "Bayswater North", Slug: "bayswater-north", Postcode: "3153", LGA: "Knox", DriveMin: 20,
		Blurb:    "The residential streets north of Canterbury Road, out towards Colchester Road. Bayswater North is a straightforward, hard-working area where computers get used until they stop, so tune-ups, replacements and moving everything across to a new machine are the bread and butter — along with printers, email and getting Wi-Fi to the back of the house.",
		MetaDesc: "At-home computer help in Bayswater North 3153 — tune-ups, new-computer setups, printers, email and Wi-Fi fixes. Friendly and local; no fix, no fee."},

	// ── Monash ──
	{Name: "Glen Waverley", Slug: "glen-waverley", Postcode: "3150", LGA: "Monash", DriveMin: 20,
		Blurb:    "The Glen, Kingsway and a lot of busy households with students and professionals under one roof. Glen Waverley visits are often about capacity — Wi-Fi that copes with everyone streaming and studying at once, a printer that works from every device, and a shared computer that's kept fast and safe.",
		MetaDesc: "Computer help at your place in Glen Waverley 3150 — high-capacity home Wi-Fi, printers, shared PCs, email, security and repairs. Local, no fix no fee.",
		Photo:    &Photo{Artist: "Dragan Jankovic Faza…", Licence: "CC BY-SA 3.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/3.0", Source: "https://commons.wikimedia.org/wiki/File:Glen_Waverley-Dragan_Jankovic_Fazan_-_panoramio.jpg"}},
	{Name: "Mount Waverley", Slug: "mount-waverley", Postcode: "3149", LGA: "Monash", DriveMin: 22,
		Blurb:    "Mount Waverley's settled streets and village shops are home to a lot of families and a lot of long-time residents. I do everything from a fast new-laptop handover to a slow, patient sit-down over email and photos, and I'm always happy to explain what I've done in plain English so it makes sense next time.",
		MetaDesc: "At-home computer help in Mount Waverley 3149 — new laptops, email, photos, Wi-Fi, printers, scam clean-ups and patient tuition. Local, no fix no fee."},
}
