package main

// Tech is one tool or platform on the /software-development stack list.
type Tech struct {
	Name string
	URL  string // vendor site; empty renders as plain text
	Note string // what it's used for, in the customer's language
}

// TechGroup is one column of the stack list, grouped by what the tools do.
type TechGroup struct {
	Kicker string
	Items  []Tech
}

// techStack is what I actually build on and what I recommend to clients.
// Names are used nominatively (plain text, no logos) — see README → Branding.
var techStack = []TechGroup{
	{
		Kicker: "Building it",
		Items: []Tech{
			{
				Name: "Go",
				URL:  "https://go.dev",
				Note: "The language everything here is written in. It compiles to a single fast program with no runtime to keep patched — which is why these sites stay up and stay cheap to host.",
			},
			{
				Name: "HTMX",
				URL:  "https://htmx.org",
				Note: "Gives pages the slick, no-reload feel of an app without a heavy JavaScript framework. Less code means fewer bugs and a smaller bill.",
			},
			{
				Name: "SQLite",
				URL:  "https://sqlite.org",
				Note: "The database. It lives inside the app, backs up as a single file, and comfortably handles a small business's workload.",
			},
			{
				Name: "Claude, by Anthropic",
				URL:  "https://www.anthropic.com/claude",
				Note: "I write code with Claude Code, and I use the Claude API for features that need real language understanding — like the instant estimate on this site's quote page.",
			},
		},
	},
	{
		Kicker: "Hosting & network",
		Items: []Tech{
			{
				Name: "Linode (Akamai)",
				URL:  "https://www.linode.com",
				Note: "The servers your site or app actually runs on. Flat monthly pricing, no reseller mark-up, and I manage them myself.",
			},
			{
				Name: "Caddy",
				URL:  "https://caddyserver.com",
				Note: "The web server out front. It renews your HTTPS certificate automatically, so your site never goes \"not secure\" over a long weekend.",
			},
			{
				Name: "Cloudflare",
				URL:  "https://www.cloudflare.com",
				Note: "DNS, spam-bot filtering and protection from attack traffic. The free plan covers most small businesses.",
			},
			{
				Name: "GitHub",
				URL:  "https://github.com",
				Note: "Where your source code lives, with tests and deployment running automatically on every change. The repository is yours.",
			},
		},
	},
	{
		Kicker: "Email, payments & messaging",
		Items: []Tech{
			{
				Name: "Amazon Web Services",
				URL:  "https://aws.amazon.com",
				Note: "SES sends the transactional email — confirmations, receipts, invoices — and S3 holds the nightly off-site backups.",
			},
			{
				Name: "Resend",
				URL:  "https://resend.com",
				Note: "A simpler alternative to SES for app email. Worth the small premium when you want a dashboard rather than configuration.",
			},
			{
				Name: "Stripe",
				URL:  "https://stripe.com",
				Note: "Card payments, subscriptions and payment links. Quick to set up and it settles straight into your bank account.",
			},
			{
				Name: "Twilio",
				URL:  "https://www.twilio.com",
				Note: "SMS reminders and notifications, and phone numbers when a project needs one. Reminders alone pay for themselves in no-shows avoided.",
			},
		},
	},
	{
		Kicker: "Platforms & data",
		Items: []Tech{
			{
				Name: "Shopify",
				URL:  "https://www.shopify.com",
				Note: "Where I point most retailers. If you're selling physical products, a Shopify store set up properly beats a custom build on both price and time.",
			},
			{
				Name: "Google Cloud",
				URL:  "https://cloud.google.com",
				Note: "Calendar, sign-in and Maps APIs — so a booking system can write into the Google Calendar you already use.",
			},
			{
				Name: "Mappify",
				URL:  "https://mappify.io",
				Note: "Australian address autocomplete and geocoding. More accurate here, and far cheaper, than the global alternatives.",
			},
		},
	},
}
