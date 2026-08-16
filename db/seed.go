package db

func seedOptions() error {
	type opt struct {
		Value     string
		Name      string
		Cost      int
		CostLabel string
		IsDefault bool
	}
	type grp struct {
		Name    string
		Label   string
		Hint    string
		Options []opt
	}

	seed := []grp{
		{"feature_email", "Email", "", []opt{
			{"none", "None", 0, "$0", true},
			{"send_only", "Send only", 200, "+$200", false},
			{"send_receive", "Send & receive", 1500, "+$1,000", false},
		}},
		{"feature_sms", "SMS", "", []opt{
			{"none", "None", 0, "$0", true},
			{"send_only", "Send only", 200, "+$200", false},
			{"send_receive", "Send & receive", 2000, "+$1,000", false},
		}},
		{"feature_login", "Login / Authentication", "", []opt{
			{"none", "None", 0, "$0", true},
			{"google_only", "Google only", 30, "+$30", false},
			{"password_google", "Username/password + Google", 1500, "+$900", false},
			{"sso", "Single Sign-On (SSO)", 3000, "+$3,000", false},
		}},
		{"feature_roles", "Roles", "How many user roles does the application need?", []opt{
			{"1", "Public only", 0, "$0", true},
			{"2", "Public + Admin", 300, "+$300", false},
			{"3", "Public + Admin + Registered", 600, "+$600", false},
			{"4+", "4 or more", 1000, "+$1000", false},
		}},
		{"feature_media", "Images & Video", "", []opt{
			{"none", "None / static", 0, "$0", true},
			{"upload", "Can upload", 200, "+$200", false},
			{"upload_ai", "Upload with AI processing", 400, "+$400", false},
		}},
		{"feature_docs", "Documents", "", []opt{
			{"none", "None / static", 0, "$0", true},
			{"upload", "Can upload", 200, "+$200", false},
			{"upload_ai", "Upload with AI processing", 400, "+$400", false},
		}},
		{"feature_domain", "Domain", "", []opt{
			{"we_supply", "We supply / control", 0, "+$0", true},
			{"you_supply", "You supply / control", 500, "$500", false},
		}},
		{"feature_users", "Users", "", []opt{
			{"1", "1 person", 0, "$0", true},
			{"2-10", "2-10 people", 200, "+$200", false},
			{"10-200", "10-200 people", 500, "+$1,000", false},
			{"1000+", "1,000+ people", 1000, "+$3,000", false},
		}},
		{"feature_payments", "Payments", "", []opt{
			{"none", "None", 0, "$0", true},
			{"stripe", "Stripe", 650, "+$650", false},
		}},
		{"feature_design", "Design", "", []opt{
			{"we_design", "We design", 0, "$0", true},
			{"you_supply", "You supply style guide", 3000, "+$3,000", false},
		}},
		{"feature_legal", "Legal & Info Pages", "Privacy policy, terms of service, FAQ, about page, help", []opt{
			{"we_supply", "We supply", 0, "$0", true},
			{"you_supply", "You supply", 500, "+$500", false},
		}},
		{"feature_security", "IT Security / Architecture Review", "Do you have an internal IT security or architecture employee who will review/approve the project?", []opt{
			{"no", "No", 0, "$0", true},
			{"yes", "Yes", 1000, "+$1,000", false},
		}},
		{"feature_duration", "Project Duration", "", []opt{
			{"ongoing", "Ongoing", 0, "$0/yr", true},
			{"one_off", "One-off event / exercise", 500, "+$500", false},
		}},
		{"feature_data", "Initial Data Load", "Is there initial data you control that needs to be loaded?", []opt{
			{"no", "No", 0, "$0", true},
			{"yes", "Yes", 800, "+$800", false},
		}},
		{"feature_revisions", "Revisions", "How many rounds of revisions are included?", []opt{
			{"2", "2", 0, "$0", true},
			{"3", "3", 120, "+$120", false},
			{"4", "4", 240, "+$240", false},
			{"5", "5", 360, "+$360", false},
		}},
		{"feature_apis", "API Integrations", "Aside from Google login, email, and SMS — how many additional API integrations are needed?", []opt{
			{"0", "None", 0, "$0", true},
			{"1", "1", 800, "+$800", false},
			{"2", "3", 1600, "+$1,600", false},
			{"3", "3+", 2400, "+$2,400", false},
		}},
		{"feature_security_report", "Security Report", "A report detailing the security posture of your application", []opt{
			{"yes", "Yes (included)", 0, "$0", true},
		}},
		{"feature_code_quality", "Code Quality Report", "A report on code quality, test coverage, and maintainability", []opt{
			{"yes", "Yes (included)", 0, "$0", true},
		}},
		{"feature_how_to_use", "How-to-Use Guide", "Documentation on how to use your application", []opt{
			{"yes", "Yes (included)", 0, "$0", true},
		}},
		{"feature_source_access", "Access to Source Code", "", []opt{
			{"no", "No (private GitHub repo)", 0, "$0", true},
			{"yes", "Yes (public GitHub repo)", 5000, "+$5,000", false},
		}},
		{"feature_perf_test", "Performance Test Report", "", []opt{
			{"no", "No", 0, "$0", true},
			{"yes", "Yes", 900, "+$900", false},
		}},
		{"feature_pdf", "PDF File Generation", "", []opt{
			{"no", "No", 0, "$0", true},
			{"yes", "Yes", 20, "+$20", false},
		}},
	}

	groups := make([]OptionGroup, len(seed))
	for i, s := range seed {
		groups[i] = OptionGroup{
			Name:      s.Name,
			Label:     s.Label,
			Hint:      s.Hint,
			SortOrder: i,
		}
		for j, o := range s.Options {
			groups[i].Options = append(groups[i].Options, Option{
				Value:     o.Value,
				Name:      o.Name,
				Cost:      o.Cost,
				CostLabel: o.CostLabel,
				IsDefault: o.IsDefault,
				SortOrder: j,
			})
		}
	}

	return SaveAllOptions(groups)
}
