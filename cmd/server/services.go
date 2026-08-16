package main

import "fmt"

// Service is one entry in the service catalogue. The catalogue drives the home
// page grid, the /services index, and each /services/{slug} detail page.
type Service struct {
	Slug      string   // URL slug, e.g. "email-outlook"
	Title     string   // "Email & Outlook Problems"
	Kicker    string   // short mono label shown above the card title
	Short     string   // 1–2 sentence card blurb
	Intro     string   // lead paragraph on the detail page
	Body      []string // further paragraphs
	Problems  []string // "Common problems I fix"
	PriceNote string   // service-specific pricing/duration note
	Featured  bool     // software development — gets its own page + richer treatment
	Related   []string // slugs of related services
	MetaTitle string   // <title>
	MetaDesc  string   // meta description
}

// Package is a fixed-price software development offering.
type Package struct {
	Name     string
	From     string // "$1,200" / "$120/hr"
	Blurb    string
	Includes []string
}

// URL returns the canonical path for the service page.
func (s *Service) URL() string {
	if s.Featured {
		return "/software-development"
	}
	return "/services/" + s.Slug
}

// Number returns the zero-padded position of the service in the catalogue ("01".."13").
func (s *Service) Number() string {
	for i := range services {
		if services[i].Slug == s.Slug {
			return fmt.Sprintf("%02d", i+1)
		}
	}
	return "—"
}

var servicesBySlug = map[string]*Service{}

func init() {
	for i := range services {
		servicesBySlug[services[i].Slug] = &services[i]
	}
}

func findService(slug string) (*Service, bool) {
	s, ok := servicesBySlug[slug]
	return s, ok
}

func featuredService() *Service {
	for i := range services {
		if services[i].Featured {
			return &services[i]
		}
	}
	return nil
}

func relatedServices(s *Service) []*Service {
	var out []*Service
	for _, slug := range s.Related {
		if r, ok := servicesBySlug[slug]; ok {
			out = append(out, r)
		}
	}
	return out
}

// suburbs is the published service area (roughly 30 minutes' drive from Donvale).
var suburbs = []string{
	"Donvale", "Doncaster", "Doncaster East", "Templestowe", "Templestowe Lower", "Bulleen",
	"Park Orchards", "Warrandyte", "Wonga Park", "Ringwood", "Ringwood North", "Ringwood East",
	"Heathmont", "Croydon", "Kilsyth", "Mooroolbark", "Lilydale", "Chirnside Park",
	"Mitcham", "Nunawading", "Vermont", "Vermont South", "Forest Hill", "Blackburn",
	"Box Hill", "Box Hill North", "Surrey Hills", "Balwyn", "Balwyn North", "Camberwell", "Canterbury",
	"Eltham", "Eltham North", "Montmorency", "Lower Plenty", "Greensborough", "Watsonia", "Viewbank",
	"Heidelberg", "Eaglemont", "Rosanna", "Bayswater", "Wantirna", "Boronia", "Knoxfield",
	"Wheelers Hill", "Glen Waverley", "Mount Waverley",
}

// softwarePackages are the fixed-price software development offerings.
// Prices are starting points — adjust freely.
var softwarePackages = []Package{
	{
		Name:  "Shopify store setup",
		From:  "$1,200",
		Blurb: "A ready-to-sell online store on Shopify, set up properly the first time.",
		Includes: []string{
			"Theme selection & customisation to your branding",
			"Up to 20 products loaded with images and variants",
			"Payments, shipping zones, taxes (GST) and email notifications",
			"Domain connected, SSL, and a walkthrough so you can run it yourself",
		},
	},
	{
		Name:  "Small-business website",
		From:  "$1,500",
		Blurb: "A fast, modern site that tells people what you do and how to reach you.",
		Includes: []string{
			"Up to 5 pages, mobile-first design",
			"Contact / booking / enquiry form",
			"Google Business Profile & basic local SEO setup",
			"Hosting set up and first year of updates included",
		},
	},
	{
		Name:  "Custom web app",
		From:  "$3,000",
		Blurb: "Bespoke software for the thing no off-the-shelf product quite does.",
		Includes: []string{
			"Scoping session and written spec",
			"Logins, roles, database, admin screens",
			"Hosted on infrastructure I run, with backups",
			"Source in a private repo; documentation included",
		},
	},
	{
		Name:  "App & spreadsheet modernisation",
		From:  "$900",
		Blurb: "Turn the Access database, macro-heavy spreadsheet or old desktop app into something that runs in the browser.",
		Includes: []string{
			"Assessment of what you have and what it needs to do",
			"First working version you can try with real data",
			"Migration of your existing data",
			"Iterate from there — hourly or fixed price",
		},
	},
	{
		Name:  "Integrations & automation",
		From:  "$600",
		Blurb: "Make the tools you already use talk to each other and stop re-typing things.",
		Includes: []string{
			"Xero / MYOB, Shopify, Google Workspace, Microsoft 365, email & SMS",
			"Scheduled reports, reminders and data syncs",
			"AI-assisted document processing where it genuinely helps",
		},
	},
	{
		Name:  "Hosting & maintenance",
		From:  "$30/month",
		Blurb: "Keep what I've built (or what you already have) online, patched and backed up.",
		Includes: []string{
			"Managed hosting, TLS certificates, monitoring",
			"Nightly backups",
			"Small content changes and dependency updates",
		},
	},
}

// hourlyRate is the published rate for ad-hoc software work.
const softwareHourly = "$120/hr"

// services is the catalogue, ordered roughly by how often each comes up.
var services = []Service{
	{
		Slug:   "email-outlook",
		Title:  "Email & Outlook Problems",
		Kicker: "Email",
		Short:  "Can't send or receive? Outlook won't open? Moving to a new provider or a new PC? Email is the number one thing I fix.",
		Intro:  "Email breaks in a hundred small ways — a password change at your provider, an Outlook profile that won't load, a mailbox that quietly filled up. I sort it out at your place, and make sure it works on your phone as well as your computer.",
		Body: []string{
			"I work with Outlook (all versions and the new Outlook), Microsoft 365, Gmail, Yahoo and ISP accounts such as Telstra/Bigpond, Optus and iPrimus — including the forced migrations those providers spring on people from time to time.",
			"If you're moving to a new computer, I bring your mail, contacts and calendar across so nothing is lost, and set the same accounts up on your phone and tablet.",
		},
		Problems: []string{
			"Can't send or receive; constant password prompts",
			"Outlook won't open, crashes, or sits on a spinning wheel",
			"Telstra/Bigpond, Optus or iPrimus account changes and shutdowns",
			"Moving email to Microsoft 365, or onto a new PC",
			"Gmail, Yahoo or ymail on the desktop alongside Outlook",
			"Mailbox full, missing folders, vanished emails or lost contacts",
			"Email not syncing between phone, tablet and computer",
		},
		PriceNote: "Most email problems take 30–60 minutes.",
		Related:   []string{"scam-virus-security", "new-computer-setup", "one-on-one-help"},
		MetaTitle: "Email & Outlook Help — Donvale, Doncaster & Melbourne's East | James McHugh",
		MetaDesc:  "Outlook won't open, can't send or receive, moving to Microsoft 365? Same-day email fixes at your place around Donvale VIC. No fix, no fee.",
	},
	{
		Slug:   "printers-scanners",
		Title:  "Printers & Scanners",
		Kicker: "Printing",
		Short:  "Printer showing offline, stopped working after a router or Windows change, or a new one still in the box — I'll get it printing and scanning from every device.",
		Intro:  "Printers fail for boring reasons: a new modem, a Windows update, a driver that quietly broke. I find the actual cause rather than reinstalling the same thing three times.",
		Body: []string{
			"Epson, Canon, HP, Brother — Wi-Fi, USB or on an office network. I'll set up printing and scan-to-computer from your PC, laptop, phone and tablet, and show you how to keep it going.",
		},
		Problems: []string{
			"Printer shows offline or documents just sit in the queue",
			"Stopped printing after a new router, modem or Windows update",
			"Prints from your phone but not from the computer (or the other way round)",
			"New printer setup, wireless connection and scanning to PC",
			"Double-sided, colour or paper-size settings gone wrong",
			"Shared office printer not visible to some computers",
		},
		PriceNote: "Typical printer fix is 30–45 minutes onsite.",
		Related:   []string{"wifi-internet-networking", "new-computer-setup", "small-business-it"},
		MetaTitle: "Printer Not Printing? Printer & Scanner Setup — Donvale & Eastern Suburbs | James McHugh",
		MetaDesc:  "Printer offline, won't print after a router change, or new printer setup. Onsite printer and scanner help around Donvale, Doncaster and Ringwood. No fix, no fee.",
	},
	{
		Slug:   "scam-virus-security",
		Title:  "Scams, Viruses & Security Checks",
		Kicker: "Security",
		Short:  "Clicked a dodgy link or let someone 'from Microsoft' onto your computer? I'll clean it, secure your accounts, and give you a written all-clear for your bank.",
		Intro:  "If someone has had remote access to your computer, or you've had fake virus pop-ups take over the screen, the priority is simple: make it safe again, lock down your accounts, and give you the confidence to keep using it.",
		Body: []string{
			"I remove malware and unwanted remote-access tools, check what was touched, change and secure passwords, set up two-factor authentication, and confirm in writing that the machine is clean — banks often ask for exactly that before restoring access.",
			"I'll also sit down with you and explain what those Norton, McAfee and Microsoft prompts actually mean, which ones are real, and what to do next time.",
		},
		Problems: []string{
			"Clicked a link or allowed remote access after a phone call",
			"Fake Microsoft, McAfee or Total AV pop-ups blocking the screen",
			"Email or Facebook account hacked, contacts receiving spam",
			"'Safe to use' letter required by your bank",
			"Antivirus renewal and billing prompts — real or scam?",
			"Two-factor authentication and password manager setup",
			"Security check of a phone or tablet",
			"Locked out — Windows password, Microsoft account or BitLocker recovery key",
		},
		PriceNote: "A full clean-up and secure is usually 1–1.5 hours. Written confirmation included.",
		Related:   []string{"email-outlook", "one-on-one-help", "phone-tablet-tv"},
		MetaTitle: "Virus & Scam Clean-up, Security Checks — Donvale & Melbourne's East | James McHugh",
		MetaDesc:  "Remote-access scam, fake pop-ups, hacked email? Malware removal, account lock-down, 2FA and a written all-clear for your bank. Onsite around Donvale VIC.",
	},
	{
		Slug:   "wifi-internet-networking",
		Title:  "Wi-Fi, Internet & Home Networking",
		Kicker: "Network",
		Short:  "No internet, Wi-Fi that drops out in half the house, a new modem still in the box, or a network drive nobody can find — sorted.",
		Intro:  "Your provider says the internet is fine, but nothing in the house works properly. I look at the whole picture — modem, router, mesh, cabling and each device — and fix the part that's actually broken.",
		Body: []string{
			"I set up new NBN modems and Telstra/Optus boxes, extend coverage with mesh where it genuinely helps, get smart TVs and streaming boxes online, and sort out shared drives and NAS units so everyone can reach them.",
		},
		Problems: []string{
			"No internet, or intermittent drop-outs on one device or all of them",
			"Weak Wi-Fi in parts of the house; mesh extenders not helping",
			"New modem or router setup and Wi-Fi migration",
			"Devices won't connect after a password or router change",
			"Network drive, NAS or shared folder not accessible",
			"Smart TV, Google Home or streaming box won't connect",
			"Small office: cabling, switches, and getting every PC on the network",
		},
		PriceNote: "Most Wi-Fi and modem jobs are 45–90 minutes onsite.",
		Related:   []string{"printers-scanners", "phone-tablet-tv", "small-business-it"},
		MetaTitle: "Wi-Fi & Internet Problems Fixed — Donvale, Templestowe, Ringwood | James McHugh",
		MetaDesc:  "No internet, patchy Wi-Fi, new modem setup, network drives. Onsite home and small-office networking help around Donvale VIC. No fix, no fee.",
	},
	{
		Slug:   "computer-repairs",
		Title:  "Computer & Laptop Repairs",
		Kicker: "Repairs",
		Short:  "Won't turn on, won't boot, stuck on an update, running slow or crashing — desktops, laptops and gaming PCs, fixed at your place where possible.",
		Intro:  "Before you replace it, let me look at it. A dead PC after a power outage, a laptop with a blank screen, or a machine that takes ten minutes to log in is often a cheap, quick fix.",
		Body: []string{
			"I diagnose hardware and software faults onsite: power supplies, memory, drives, screens, keyboards, batteries and thermal problems, plus Windows repairs, stuck updates and BitLocker recovery. I'll install parts you've bought (SSD, NVMe, RAM, GPU, PSU) or source them for you.",
			"If it's genuinely not worth repairing, I'll tell you plainly, help you choose a replacement, and move your data across.",
		},
		Problems: []string{
			"Dead — no power or no display, often after a storm or outage",
			"Boots but no login screen, blue screens, or stuck on 'working on updates'",
			"Slow start-up and general sluggishness — SSD upgrade & tune-up",
			"Laptop keyboard, battery, screen, hinge or charging faults",
			"Gaming PC crashing under load, overheating, custom builds",
			"Part installs: SSD/NVMe, RAM, graphics card, power supply",
			"'Out of space' warnings, sound not working, external monitor not detected",
		},
		PriceNote: "Diagnosis onsite; you'll get a clear yes/no and a price before parts are ordered.",
		Related:   []string{"data-recovery-backup", "new-computer-setup", "linux-custom-builds"},
		MetaTitle: "Computer & Laptop Repairs at Your Place — Donvale & Melbourne's East | James McHugh",
		MetaDesc:  "PC won't turn on, won't boot, stuck on update, running slow? Onsite desktop, laptop and gaming PC repairs around Donvale, Doncaster and Ringwood. No fix, no fee.",
	},
	{
		Slug:   "new-computer-setup",
		Title:  "New Computer Setup & Data Transfer",
		Kicker: "Setup",
		Short:  "Bought a new PC or laptop? I'll set it up properly, move everything from the old one, and get email, printer and Office working before I leave.",
		Intro:  "A new computer should feel like your old one — only faster. I move your files, photos, email, favourites and printers across, remove the bloatware, and set up backups so this is the last time you ever worry about losing anything.",
		Body: []string{
			"I also handle Windows 11 upgrades and compatibility checks for older machines, Microsoft 365 installs, and setting up separate work and personal accounts so your data stays where it belongs.",
		},
		Problems: []string{
			"Unbox, set up and personalise a new desktop, laptop or all-in-one",
			"Transfer files, photos, Outlook email and favourites from the old computer",
			"Windows 11 upgrade, or 'is my PC compatible?' check",
			"Microsoft 365 / Office install and licensing",
			"Printer, scanner and Wi-Fi reconnected",
			"Backup (OneDrive, external drive) set up so it just happens",
			"Advice on what to buy — I'll tell you what you actually need",
		},
		PriceNote: "Setup with data transfer is typically 1.5–2.5 hours depending on how much is coming across.",
		Related:   []string{"email-outlook", "data-recovery-backup", "printers-scanners"},
		MetaTitle: "New Computer Setup & Data Transfer — Donvale, Doncaster & Surrounds | James McHugh",
		MetaDesc:  "New PC or laptop setup, data and Outlook transfer, Windows 11 upgrades and Office install, done at your place around Donvale VIC.",
	},
	{
		Slug:   "data-recovery-backup",
		Title:  "Data Recovery & Backup",
		Kicker: "Data",
		Short:  "Old laptop won't boot but the photos are on it? I'll get your data off, and set up a backup so it never happens again.",
		Intro:  "Most 'dead' computers still have perfectly readable drives. I recover documents, photos and email from machines that won't start, from old towers before they go to e-waste, and from failing external drives.",
		Body: []string{
			"I'll then set up something automatic — OneDrive, Google Drive or an external drive — so your files are safe without you having to remember. For physically damaged drives that need a clean room I'll tell you honestly and point you to a specialist rather than make it worse.",
		},
		Problems: []string{
			"Getting files off a laptop or PC that won't boot",
			"Backing up an old computer before it's thrown out",
			"External hard drive or USB stick not showing up",
			"Setting up OneDrive / Google Drive / external backups properly",
			"Cloning a drive to a new SSD",
			"Recovering photos from an old phone or camera card",
		},
		PriceNote: "Simple recoveries are usually 1–2 hours. No data recovered, no labour charged.",
		Related:   []string{"computer-repairs", "new-computer-setup"},
		MetaTitle: "Data Recovery & Backup Setup — Donvale & Melbourne's East | James McHugh",
		MetaDesc:  "Get photos and files off a computer that won't start, and set up backups that actually run. Onsite around Donvale, Doncaster and Eltham. No data, no fee.",
	},
	{
		Slug:   "phone-tablet-tv",
		Title:  "Phones, Tablets & Smart TVs",
		Kicker: "Devices",
		Short:  "Email not arriving on your iPhone, pop-ups on your Android, an app you can't find on the TV — I help with the small screens too.",
		Intro:  "Computers aren't the only thing that misbehave. I set up email and accounts on phones and tablets, clear out the aftermath of a bad link, and get apps like SBS On Demand or Netflix working on the TV.",
		Body: []string{
			"I'll also untangle Google and Apple account problems — the ones where changing one password signs you out of the speaker, the TV and the doorbell all at once.",
		},
		Problems: []string{
			"Email (Hotmail, Gmail, Bigpond) not working on iPhone or Android",
			"Pop-ups and 'virus' warnings after tapping a link",
			"Google or Apple account sign-in cascades across devices",
			"Installing and signing into apps on a smart TV",
			"Photos and contacts syncing between phone and computer",
			"Setting up a new phone or tablet and moving everything over",
		},
		PriceNote: "Often 30–45 minutes; can be combined with a computer visit.",
		Related:   []string{"email-outlook", "scam-virus-security", "one-on-one-help"},
		MetaTitle: "iPhone, Android, Tablet & Smart TV Help — Donvale & Surrounds | James McHugh",
		MetaDesc:  "Phone email not working, pop-ups after a bad link, apps on the smart TV. Patient device help at home around Donvale VIC.",
	},
	{
		Slug:   "one-on-one-help",
		Title:  "Patient One-on-One Help",
		Kicker: "Tuition",
		Short:  "Sit-down sessions at your own pace: what's safe to click, how to bookmark, using new apps — seniors especially welcome.",
		Intro:  "Sometimes nothing is broken; you just want someone to explain it properly, without jargon and without rushing. I'll sit with you, answer the questions you've been putting off, and leave you notes.",
		Body: []string{
			"Popular topics: understanding antivirus and Windows prompts, spotting scam emails, organising bookmarks and files, video calling family, using ChatGPT or Claude, audiobook and streaming apps, and getting comfortable with a new device.",
		},
		Problems: []string{
			"Which prompts and emails are real, and what to ignore",
			"Bookmarks, favourites and finding your files again",
			"Setting up and using apps like Claude, ChatGPT, audiobook and streaming apps",
			"Video calls, photos and messaging with family",
			"Ongoing regular visits if you'd like them",
			"Written notes left behind so you can do it yourself next time",
		},
		PriceNote: "Book a one-hour session; longer or regular sessions available.",
		Related:   []string{"scam-virus-security", "phone-tablet-tv", "new-computer-setup"},
		MetaTitle: "Patient Computer Help for Seniors & Beginners — Donvale & Melbourne's East | James McHugh",
		MetaDesc:  "One-on-one computer tuition at home: what's safe to click, bookmarks, new apps, video calls. Patient help around Donvale, Doncaster and Ringwood.",
	},
	{
		Slug:   "small-business-it",
		Title:  "Small Business IT",
		Kicker: "Business",
		Short:  "Staff laptops, Microsoft 365, business email on your own domain, shared files and office printers — practical IT for small teams without a retainer.",
		Intro:  "Small businesses need someone who turns up, fixes it, and explains what they did — not a help desk ticket. I set up and support the everyday IT of shops, practices, tradies and home offices.",
		Body: []string{
			"Setting up new staff laptops with Microsoft 365, moving to business email on your own domain, shared drives, backups, printers and Wi-Fi across the office, and honest advice on tools like ticketing, booking and invoicing systems. When it makes sense to build something custom, see software development.",
		},
		Problems: []string{
			"New staff computers set up with Microsoft 365 and your apps",
			"Business email on your own domain; migrating from ISP mailboxes",
			"Shared drives, OneDrive/SharePoint, and backups",
			"Office printers, scanners and Wi-Fi that every PC can reach",
			"Advice on booking, ticketing, POS and invoicing tools",
			"Ad-hoc support — no lock-in contract",
		},
		PriceNote: "Same rates as home visits. Ask about a bundle for multiple machines.",
		Related:   []string{"software-development", "email-outlook", "wifi-internet-networking"},
		MetaTitle: "Small Business IT Support — Donvale, Doncaster & Melbourne's East | James McHugh",
		MetaDesc:  "Microsoft 365, business email, staff laptops, printers and networks for small businesses around Donvale VIC. Onsite, no retainer.",
	},
	{
		Slug:   "linux-custom-builds",
		Title:  "Linux & Custom Builds",
		Kicker: "Linux",
		Short:  "Ubuntu or Kubuntu installs, dual-boot, custom PC assembly and part upgrades — for people who want something other than the default.",
		Intro:  "If you'd rather run Linux, or you've bought the parts and want them put together properly, I do that too. I've run Linux servers for years and build my own machines.",
		Body: []string{
			"Installs and upgrades of Ubuntu, Kubuntu, Mint and friends; rescuing a Linux system after a failed update; dual-boot with Windows; custom PC assembly and GPU/PSU/cooling swaps; and getting a NAS or home server behaving.",
		},
		Problems: []string{
			"Fresh Linux install or dual-boot with Windows",
			"Linux system broken after an update — repair or recover data",
			"Custom PC assembly, GPU/PSU/RAM/storage swaps",
			"NAS, home server and router configuration",
		},
		PriceNote: "Standard rates; part installs are usually 45–90 minutes.",
		Related:   []string{"computer-repairs", "wifi-internet-networking", "software-development"},
		MetaTitle: "Linux Installs & Custom PC Builds — Donvale & Melbourne's East | James McHugh",
		MetaDesc:  "Ubuntu/Kubuntu installs, dual-boot, custom PC assembly and part upgrades, done at your place around Donvale VIC.",
	},
	{
		Slug:     "software-development",
		Title:    "Software Development",
		Kicker:   "Featured",
		Featured: true,
		Short:    "Shopify stores, small-business websites, custom web apps, and bringing old apps and spreadsheets into the browser. Designed, built and hosted by me.",
		Intro:    "Alongside computer help I design, build and host software for small businesses — from a Shopify store to a bespoke web application that replaces a tangle of spreadsheets.",
		Body: []string{
			"One developer, no agency layers: you talk directly to the person writing the code, which keeps costs down and turnaround fast.",
			"Everything I build can be hosted and maintained on infrastructure I run, so it stays online, backed up and up to date without you thinking about it.",
		},
		Problems: []string{
			"Shopify store setup, theme customisation and app integration",
			"Small-business websites with booking, forms and payments",
			"Custom web apps — dashboards, portals, internal tools",
			"Updating or rewriting legacy apps and spreadsheet-based processes",
			"Integrations and automation between the tools you already use",
			"Hosting, backups, monitoring and ongoing maintenance",
		},
		PriceNote: "Hourly work " + softwareHourly + "; fixed-price packages from $600. Larger projects: get a written proposal via the quote tool.",
		Related:   []string{"small-business-it", "one-on-one-help"},
		MetaTitle: "Software Development — Shopify, Websites & Custom Web Apps | James McHugh",
		MetaDesc:  "Shopify setup, small-business websites, custom web apps and legacy app rewrites, built and hosted in Melbourne's east. Fixed packages or $120/hr.",
	},
}
