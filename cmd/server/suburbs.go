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
var lgaOrder = []string{"Manningham", "Whitehorse", "Maroondah", "Boroondara", "Nillumbik", "Banyule", "Yarra Ranges", "Knox", "Monash"}

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
		MetaDesc: "Same-day at-home computer help in Doncaster East 3109 — email, printers, Wi-Fi, slow computers, one-on-one help. Five minutes from Donvale, no call-out fee.",
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

	// ── Maroondah ──
	{Name: "Ringwood", Slug: "ringwood", Postcode: "3134", LGA: "Maroondah", DriveMin: 12,
		Blurb:    "Ringwood is a quick run out along Maroondah Highway — Eastland, the station precinct and the residential streets around Ringwood Lake. There's a bit of everything here: home users, apartments, and small businesses that need reliable email, backups and a printer that works from every device.",
		MetaDesc: "Computer help at your place in Ringwood 3134 — printers, Wi-Fi, email, backups, virus and scam clean-ups, PC repairs. Local, same or next day where possible.",
		Photo:    &Photo{Artist: "Unknown", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Ringwood_Cloclocktower.JPG"}},
	{Name: "Ringwood North", Slug: "ringwood-north", Postcode: "3134", LGA: "Maroondah", DriveMin: 10,
		Blurb:    "Bordering Warrandyte and Park Orchards, Ringwood North has larger homes and plenty of gardens between you and the modem. Whole-house Wi-Fi is the classic job, along with new-computer setups and getting older machines running smoothly again. Being so close to Donvale, it's usually an easy same-day visit.",
		MetaDesc: "At-home Wi-Fi and computer help in Ringwood North 3134 — whole-house Wi-Fi, PC tune-ups, setups, email and printers. Close to Donvale, no call-out fee.",
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

	// ── Yarra Ranges ──
	{Name: "Kilsyth", Slug: "kilsyth", Postcode: "3137", LGA: "Yarra Ranges", DriveMin: 20,
		Blurb:    "Out along Colchester Road at the foot of the hills, Kilsyth is a mix of family homes, larger blocks and light industry. Home users here often want the basics sorted properly — Wi-Fi, email, printer, backup — and the small workshops and trades want a computer setup that keeps the invoicing and quoting running without drama.",
		MetaDesc: "Computer help at home or work in Kilsyth 3137 — Wi-Fi, email, printers, backups and small-business setups. Friendly, local, no fix no fee.",
		Photo:    &Photo{Artist: "Matthew Paul Argall", Licence: "CC BY 2.0", LicenceURL: "https://creativecommons.org/licenses/by/2.0", Source: "https://commons.wikimedia.org/wiki/File:Montgomery_Court,_Kilsyth.jpg"}},
	{Name: "Mooroolbark", Slug: "mooroolbark", Postcode: "3138", LGA: "Yarra Ranges", DriveMin: 20,
		Blurb:    "Mooroolbark's a big, family-oriented suburb around the station and Brice Avenue, with plenty of homes that have grown a tangle of devices over the years. Getting everything on the one Wi-Fi network, backing up the photos and giving the main computer a good clean-out are the regular jobs.",
		MetaDesc: "At-home computer help in Mooroolbark 3138 — Wi-Fi for every device, backups, PC clean-outs, email, printers and phone/tablet help. No fix, no fee.",
		Photo:    &Photo{Artist: "Phenix1888", Licence: "CC BY-SA 4.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/4.0", Source: "https://commons.wikimedia.org/wiki/File:Mooroolbark_station_front.jpg"}},
	{Name: "Lilydale", Slug: "lilydale", Postcode: "3140", LGA: "Yarra Ranges", DriveMin: 25,
		Blurb:    "Lilydale is about as far east as I regularly go — the end of the train line and the gateway to the Valley. There's a lot of history here and a lot of homes with years of photos and documents on a single ageing computer, so backups and data recovery come up often, alongside the everyday email, printer and Wi-Fi jobs.",
		MetaDesc: "Computer help at your place in Lilydale 3140 — backups, data recovery, slow PCs, email, printers and Wi-Fi. Local eastern-suburbs tech, no fix no fee.",
		Photo:    &Photo{Artist: "Melburnian", Licence: "CC BY 3.0", LicenceURL: "https://creativecommons.org/licenses/by/3.0", Source: "https://commons.wikimedia.org/wiki/File:Lilydale_Maroondah_Highway.jpg"}},
	{Name: "Chirnside Park", Slug: "chirnside-park", Postcode: "3116", LGA: "Yarra Ranges", DriveMin: 20,
		Blurb:    "Around the shopping centre and the newer estates towards the Yarra, Chirnside Park has plenty of family homes with modern layouts that don't always suit a single modem in the corner. Mesh Wi-Fi, smart-TV and streaming setups and getting the kids' devices under control are frequent calls.",
		MetaDesc: "At-home Wi-Fi and computer help in Chirnside Park 3116 — mesh Wi-Fi, smart TVs and streaming, PCs, printers and email. Friendly, local, no fix no fee.",
		Photo:    &Photo{Artist: "Ottre", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Edward_Road,_Chirnside2.jpg"}},

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
	{Name: "Camberwell", Slug: "camberwell", Postcode: "3124", LGA: "Boroondara", DriveMin: 22,
		Blurb:    "Camberwell's a bit further in, around the Junction and the shopping strip, but well within range. Lots of professionals working from home a few days a week and wanting a proper setup — a second screen, a printer that works, video calls that don't drop — plus long-time locals who'd like their computer looked after by the same person each time.",
		MetaDesc: "Home-office and household computer help in Camberwell 3124 — reliable Wi-Fi, printers, screens, email and repairs at your place. No fix, no fee.",
		Photo:    &Photo{Artist: "User:Orderinchaos", Licence: "CC BY-SA 3.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/3.0", Source: "https://commons.wikimedia.org/wiki/File:Burke_Rd_S_from_Camberwell_shops.jpg"}},
	{Name: "Canterbury", Slug: "canterbury", Postcode: "3126", LGA: "Boroondara", DriveMin: 20,
		Blurb:    "Maling Road, the station and some of the loveliest old homes in the east. Canterbury's period houses can be stubborn about Wi-Fi, and its residents often value a slow, careful explanation over a quick fix. I'm happy to sit down, sort the problem and show you what changed.",
		MetaDesc: "Patient at-home computer help in Canterbury 3126 — Wi-Fi in period homes, email, printers, scam protection and one-on-one help. Local, no fix no fee.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Maling_Road_looking_north-east,_Canterbury.jpg"}},

	// ── Nillumbik ──
	{Name: "Eltham", Slug: "eltham", Postcode: "3095", LGA: "Nillumbik", DriveMin: 20,
		Blurb:    "Bushy blocks, mud-brick houses and a strong creative community around the town centre and Montsalvat. Eltham homes are often spread out and full of trees, so Wi-Fi coverage and reliable internet are the big ones, along with backups for the many artists, writers and home businesses working from the house.",
		MetaDesc: "Computer, internet and Wi-Fi help at home in Eltham 3095 — coverage across bush blocks, backups, home-office setups, PCs and printers. No fix, no fee.",
		Photo:    &Photo{Artist: "Marcnut1996", Licence: "CC BY-SA 4.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/4.0", Source: "https://commons.wikimedia.org/wiki/File:Eltham_Trestle_Bridge_September_2023_1.jpg"}},
	{Name: "Eltham North", Slug: "eltham-north", Postcode: "3095", LGA: "Nillumbik", DriveMin: 22,
		Blurb:    "Further up the hill from Eltham, quieter and more spread out. Similar story to its neighbour — larger properties, patchy signal, and households who've built up years of photos and files that deserve a proper backup. I'll set up something that just runs, and show you how to check it.",
		MetaDesc: "At-home computer help in Eltham North 3095 — Wi-Fi across large blocks, backups that actually run, PCs, printers and email. Local and friendly.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Diamond_Creek_Trail,_Eltham_North.jpg"}},

	// ── Banyule ──
	{Name: "Montmorency", Slug: "montmorency", Postcode: "3094", LGA: "Banyule", DriveMin: 22,
		Blurb:    "A village feel around Were Street and the station, with leafy streets running down to the Plenty River. Montmorency's a friendly, settled place with a good mix of young families and long-time residents, and the calls reflect that — kids' devices and Wi-Fi one day, a patient email or scam-call sit-down the next.",
		MetaDesc: "Computer help at your place in Montmorency 3094 — Wi-Fi, family devices, email, scam clean-ups, printers and repairs. Friendly, local, no fix no fee.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Plenty_River,_Montmorency.jpg"}},
	{Name: "Lower Plenty", Slug: "lower-plenty", Postcode: "3093", LGA: "Banyule", DriveMin: 18,
		Blurb:    "Tucked between the Yarra and the Plenty rivers, Lower Plenty is closer than it feels — straight through Templestowe and over the bridge. Larger homes on bigger blocks mean the usual Wi-Fi coverage work, plus setting up new computers and TVs and keeping the household's photos backed up.",
		MetaDesc: "At-home computer and Wi-Fi help in Lower Plenty 3093 — coverage across large homes, new-computer and TV setups, backups, email and printers. No fix, no fee.",
		Photo:    &Photo{Artist: "BCProductions18", Licence: "CC BY-SA 4.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/4.0", Source: "https://commons.wikimedia.org/wiki/File:Lwr_Plenty_Road.jpg"}},
	{Name: "Greensborough", Slug: "greensborough", Postcode: "3088", LGA: "Banyule", DriveMin: 25,
		Blurb:    "Greensborough's a proper hub — the plaza, the station and a wide spread of homes and units around it. I come out for everything from a small business's point-of-sale and email to a household's slow computer, and there are always a few 'my printer's stopped working again' calls in the mix.",
		MetaDesc: "Computer help at your home or shop in Greensborough 3088 — slow PCs, printers, email, Wi-Fi, small-business IT and repairs. Local, no fix no fee.",
		Photo:    &Photo{Artist: "Wongm", Licence: "CC BY-SA 3.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/3.0", Source: "https://commons.wikimedia.org/wiki/File:Greensborough-overall-aerial.jpg"}},
	{Name: "Watsonia", Slug: "watsonia", Postcode: "3087", LGA: "Banyule", DriveMin: 25,
		Blurb:    "A compact, friendly suburb around the Watsonia shops and station. Plenty of long-time residents here who like having the same person look after their computer, phone and tablet, and who appreciate a plain-English explanation of what went wrong and how to avoid it next time.",
		MetaDesc: "At-home computer help in Watsonia 3087 — PCs, phones and tablets, email, printers, scam clean-ups and patient one-on-one help. Local, no fix no fee.",
		Photo:    &Photo{Artist: "BobTanGo", Licence: "CC BY 4.0", LicenceURL: "https://creativecommons.org/licenses/by/4.0", Source: "https://commons.wikimedia.org/wiki/File:Watsonia_facing_north,_with_the_Northeast_Link_Big_Build.jpg"}},
	{Name: "Viewbank", Slug: "viewbank", Postcode: "3084", LGA: "Banyule", DriveMin: 22,
		Blurb:    "Quiet streets on the hill above the Yarra flats and Banyule Flats reserve. Viewbank households are often busy families with a shared computer and a fleet of devices — I sort out the Wi-Fi so everything works everywhere, get the printer behaving, and make sure the family photos are backed up somewhere safe.",
		MetaDesc: "Computer help at your place in Viewbank 3084 — family Wi-Fi, printers, photo backups, PC repairs and setups. Friendly, local, no fix no fee.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Banyule_Swamp,_Viewbank.jpg"}},
	{Name: "Heidelberg", Slug: "heidelberg", Postcode: "3084", LGA: "Banyule", DriveMin: 22,
		Blurb:    "Heidelberg's got the hospital precinct, Burgundy Street's shops and cafes, and a lot of homes and units in between. I visit households, home offices and small businesses here — email that won't sync, a laptop that's on its last legs, a Wi-Fi network that drops out at the worst moment.",
		MetaDesc: "Computer help at your home or business in Heidelberg 3084 — email, laptops, Wi-Fi drop-outs, printers, backups and repairs. Local, no fix no fee.",
		Photo:    &Photo{Artist: "Bananabones", Licence: "CC BY-SA 4.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/4.0", Source: "https://commons.wikimedia.org/wiki/File:Heidelberg_Station_View.jpg"}},
	{Name: "Eaglemont", Slug: "eaglemont", Postcode: "3084", LGA: "Banyule", DriveMin: 22,
		Blurb:    "The hilltop village and its heritage streets. Eaglemont homes are beautiful and, like a lot of older houses, not built with Wi-Fi in mind. I'll get the signal to every room, help with email, banking and staying safe online, and take my time explaining things — no rush and no jargon.",
		MetaDesc: "Patient at-home computer help in Eaglemont 3084 — Wi-Fi in older homes, email, safe online banking, scam protection and one-on-one help. No fix, no fee.",
		Photo:    &Photo{Artist: "Ottre", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:House_at_Eaglemont1.jpg"}},
	{Name: "Rosanna", Slug: "rosanna", Postcode: "3084", LGA: "Banyule", DriveMin: 22,
		Blurb:    "Around the Rosanna village and the parklands along Salt Creek. A friendly, family-oriented suburb where I do plenty of everyday household jobs — a new laptop set up with everything moved over, a printer that works from the phones, a slow computer given a new lease of life.",
		MetaDesc: "At-home computer help in Rosanna 3084 — new laptop setups, printers, Wi-Fi, slow PCs and email. Local eastern-suburbs tech, no fix no fee.",
		Photo:    &Photo{Artist: "Philip Mallis", Licence: "CC BY-SA 2.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/2.0", Source: "https://commons.wikimedia.org/wiki/File:Beetham_Parade,_Rosanna.jpg"}},

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
	{Name: "Knoxfield", Slug: "knoxfield", Postcode: "3180", LGA: "Knox", DriveMin: 22,
		Blurb:    "A quiet pocket between Wantirna South and Scoresby, with family homes and a handful of small businesses on the main roads. Knoxfield jobs are usually about the household basics — Wi-Fi to every room, a printer that prints, a family computer that isn't crawling — done properly and explained clearly.",
		MetaDesc: "At-home computer help in Knoxfield 3180 — Wi-Fi, printers, family PCs, email, setups and repairs. Local, friendly, no fix no fee.",
		Photo:    &Photo{Artist: "ozzmosis", Licence: "CC BY-SA 3.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/3.0", Source: "https://commons.wikimedia.org/wiki/File:Ferny_Creek_trail_-_panoramio.jpg"}},

	// ── Monash ──
	{Name: "Wheelers Hill", Slug: "wheelers-hill", Postcode: "3150", LGA: "Monash", DriveMin: 25,
		Blurb:    "Wheelers Hill's big family homes on the hill have plenty of rooms for Wi-Fi to get lost in. Whole-house coverage, smart-TV and streaming setups, and getting the whole family's devices working together are the common calls, plus new-computer setups when it's finally time to upgrade.",
		MetaDesc: "At-home Wi-Fi and computer help in Wheelers Hill 3150 — whole-house Wi-Fi, smart TVs, family devices, new-computer setups and repairs. No fix, no fee.",
		Photo:    &Photo{Artist: "User:Fneep", Licence: "Public domain", LicenceURL: "", Source: "https://commons.wikimedia.org/wiki/File:Jells_Park_1.JPG"}},
	{Name: "Glen Waverley", Slug: "glen-waverley", Postcode: "3150", LGA: "Monash", DriveMin: 20,
		Blurb:    "The Glen, Kingsway and a lot of busy households with students and professionals under one roof. Glen Waverley visits are often about capacity — Wi-Fi that copes with everyone streaming and studying at once, a printer that works from every device, and a shared computer that's kept fast and safe.",
		MetaDesc: "Computer help at your place in Glen Waverley 3150 — high-capacity home Wi-Fi, printers, shared PCs, email, security and repairs. Local, no fix no fee.",
		Photo:    &Photo{Artist: "Dragan Jankovic Faza…", Licence: "CC BY-SA 3.0", LicenceURL: "https://creativecommons.org/licenses/by-sa/3.0", Source: "https://commons.wikimedia.org/wiki/File:Glen_Waverley-Dragan_Jankovic_Fazan_-_panoramio.jpg"}},
	{Name: "Mount Waverley", Slug: "mount-waverley", Postcode: "3149", LGA: "Monash", DriveMin: 22,
		Blurb:    "Mount Waverley's settled streets and village shops are home to a lot of families and a lot of long-time residents. I do everything from a fast new-laptop handover to a slow, patient sit-down over email and photos, and I'm always happy to explain what I've done in plain English so it makes sense next time.",
		MetaDesc: "At-home computer help in Mount Waverley 3149 — new laptops, email, photos, Wi-Fi, printers, scam clean-ups and patient tuition. Local, no fix no fee."},
}
