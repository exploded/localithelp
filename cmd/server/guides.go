package main

import "html/template"

// Guide is a self-help article under /fix-it-yourself. Content is authored here
// (not user input) so steps may contain a little inline HTML (<strong>, <code>, <a>).
type Guide struct {
	Slug     string
	Title    string
	Kicker   string // short category label
	Level    string // "Easy" | "A bit fiddly"
	Time     string // "5 minutes"
	Summary  string // card blurb + intro
	Before   string // optional caution shown before the steps
	Steps    []template.HTML
	IfNot    []template.HTML // "If that didn't work" bullets
	StopWhen string          // when to stop and book
	Service  string          // related service slug
	MetaDesc string
}

var guidesBySlug = map[string]*Guide{}

func init() {
	for i := range guides {
		guidesBySlug[guides[i].Slug] = &guides[i]
	}
}

func findGuide(slug string) (*Guide, bool) {
	g, ok := guidesBySlug[slug]
	return g, ok
}

// RelatedService resolves the related service (may be nil).
func (g *Guide) RelatedService() *Service {
	s, _ := findService(g.Service)
	return s
}

var guides = []Guide{
	{
		Slug:    "scam-call-remote-access",
		Title:   "I let someone onto my computer or clicked a scam link — what now?",
		Kicker:  "Security",
		Level:   "Easy",
		Time:    "10 minutes",
		Summary: "Someone rang \"from Microsoft\", the bank or the NBN, and you gave them access — or a link took over the screen. Do these things now, in this order.",
		Before:  "Don't ring back any number they gave you, don't pay anything, and don't let anyone else \"check\" the computer over the phone.",
		Steps: []template.HTML{
			"<strong>Disconnect it from the internet.</strong> Pull the network cable out, or turn Wi-Fi off (or just switch the modem off at the wall). If they were still connected, this ends it.",
			"<strong>Turn the computer off</strong> and leave it off. Don't try to \"fix\" or clean anything yet — that can make it harder to see what was done.",
			"<strong>Ring your bank</strong> — using the number on the back of your card, not one from the screen — and tell them what happened. Ask them to watch or freeze the account and cancel any payment you were talked into.",
			"<strong>From a different device</strong> (your phone or another computer), change the password on your <strong>email account first</strong>, then online banking, then anything else you use that email to log in to.",
			"If you read out or typed any codes, gift-card numbers or card details, tell the bank that too.",
			"Then <a href=\"/book?service=scam-virus-security\">book a visit</a>. I'll remove whatever they installed, check what was touched, secure the accounts properly, and give you a written note that the machine is safe — banks often ask for exactly that.",
		},
		IfNot: []template.HTML{
			"If it's your phone that's affected, the same applies: don't call back, change your email password from another device, ring the bank.",
			"Report it at <a href=\"https://www.scamwatch.gov.au\" target=\"_blank\" rel=\"noopener\">scamwatch.gov.au</a> when things have calmed down — it helps others.",
		},
		StopWhen: "You've done the steps above. The clean-up itself isn't a DIY job — please leave the machine off and book.",
		Service:  "scam-virus-security",
		MetaDesc: "Gave a scam caller remote access or clicked a bad link? The exact steps to take right now — disconnect, ring the bank, change your email password — around Donvale VIC.",
	},
	{
		Slug:    "fake-virus-popup",
		Title:   "A \"virus detected\" pop-up has taken over the screen",
		Kicker:  "Security",
		Level:   "Easy",
		Time:    "5 minutes",
		Summary: "A loud page saying your PC is infected, with a phone number to call, that won't close. It's a web page, not a real warning. Here's how to get rid of it.",
		Before:  "Real Windows and antivirus warnings never ask you to phone a number. Don't call it.",
		Steps: []template.HTML{
			"Don't click anything on the page — not \"Close\", not \"Scan\", not the X inside it.",
			"Press <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>Esc</kbd> to open Task Manager. Find your browser (Edge, Chrome, Firefox) in the list, click it, then click <strong>End task</strong>.",
			"If that doesn't work, hold the <strong>power button</strong> for 10 seconds until the computer switches off. Wait a moment and turn it back on.",
			"When the browser asks whether to <em>restore pages</em>, say <strong>No</strong> — otherwise the page comes straight back.",
			"Open <strong>Windows Security</strong> (search for it in the Start menu) → Virus &amp; threat protection → <strong>Quick scan</strong>.",
		},
		IfNot: []template.HTML{
			"If it comes back on its own, or you did call the number and let them connect, follow the <a href=\"/fix-it-yourself/scam-call-remote-access\">scam call guide</a> and book a visit.",
			"Pop-ups on a phone: close all browser tabs, then clear the browser's history/site data.",
		},
		StopWhen: "It keeps returning after a restart, new toolbars or programs have appeared, or you gave anyone access.",
		Service:  "scam-virus-security",
		MetaDesc: "Fake 'virus detected' pop-up with a phone number won't close? How to close it safely with Task Manager and scan your PC — no need to call the number.",
	},
	{
		Slug:    "no-internet-restart-modem",
		Title:   "No internet — restart or reset your Telstra (or any) modem",
		Kicker:  "Wi-Fi",
		Level:   "Easy",
		Time:    "10–15 minutes",
		Summary: "The single most effective fix for a dead or flaky connection is a proper restart of the modem. If that fails, a pin-hole reset puts it back to factory settings.",
		Before:  "Check <a href=\"https://www.telstra.com.au/outages\" target=\"_blank\" rel=\"noopener\">telstra.com.au/outages</a> (or your provider's outage page) on your phone first — if the street's out, no amount of restarting helps.",
		Steps: []template.HTML{
			"<strong>Restart first.</strong> Switch the modem off at the power point (or unplug its power lead). Leave it off for a full <strong>30 seconds</strong>.",
			"Switch it back on and <strong>wait 3–5 minutes</strong>. The lights will flash while it reconnects — that's normal. You want a steady light (usually blue or green on Telstra Smart Modems). Red or orange for more than 10 minutes means it still isn't connected.",
			"Try a device that's <em>close</em> to the modem. If that works but the far end of the house doesn't, it's a coverage problem, not an outage.",
			"<strong>Still nothing? Factory reset with the pin.</strong> On the back of the modem there's a tiny recessed hole marked <em>Reset</em>. With the modem <em>on</em>, push a paperclip or SIM tool into it and hold for about <strong>10 seconds</strong> until the lights flash or change, then let go.",
			"Wait <strong>5–10 minutes</strong>. Telstra modems download their settings automatically after a reset — don't unplug it during this.",
			"Reconnect your devices using the Wi-Fi name and password printed on the <strong>sticker on the modem</strong> — a factory reset puts them back to those.",
		},
		IfNot: []template.HTML{
			"If you'd changed the Wi-Fi name or password yourself, everything in the house will need to be reconnected — that's the price of a factory reset, so try the plain restart twice before going there.",
			"Only one device won't connect? Restart <em>that</em> device, then \"forget\" the Wi-Fi network on it and join again.",
		},
		StopWhen: "The light stays red/orange after a reset with no outage reported, coverage is poor in parts of the house, or you have a mesh/extender setup that's stopped talking to the modem.",
		Service:  "wifi-internet-networking",
		MetaDesc: "No internet? How to properly restart a Telstra Smart Modem, and how to do a pin-hole factory reset if that fails — and what it changes.",
	},
	{
		Slug:    "printer-offline",
		Title:   "Printer says offline or won't print",
		Kicker:  "Printing",
		Level:   "Easy",
		Time:    "10 minutes",
		Summary: "Nine times out of ten the printer has lost the Wi-Fi network or Windows is stuck on an old job. These steps fix most of it.",
		Steps: []template.HTML{
			"Turn the <strong>printer</strong> off, wait 30 seconds, turn it on. Wait until it stops making noises and shows its normal screen or steady light.",
			"<strong>Restart the computer</strong> — properly, using Restart, not Shut down (Windows \"Shut down\" doesn't fully restart).",
			"Check the printer is on the <strong>same Wi-Fi network</strong> as the computer. Most printers show the network name in their menu under Wi-Fi or Network settings. If you've recently changed modem or Wi-Fi password, the printer needs to be reconnected — use its Wi-Fi setup menu.",
			"On the computer: <strong>Settings → Bluetooth &amp; devices → Printers &amp; scanners</strong>, click your printer, and if there's a queue of stuck documents open it and <strong>cancel all</strong>.",
			"While there, click <strong>Set as default</strong> so Windows isn't sending jobs to an old printer or \"Microsoft Print to PDF\".",
			"Try printing a test page from that same screen.",
		},
		IfNot: []template.HTML{
			"If it prints from your phone but not the computer, the printer is fine — remove it from Printers &amp; scanners and add it again.",
			"If it stopped working right after a Windows update or a new modem, that's the cause; the reconnection above usually sorts it.",
		},
		StopWhen: "It's still offline after reconnecting to Wi-Fi, scanning to the computer has stopped, or it's a shared office printer that some PCs can't see.",
		Service:  "printers-scanners",
		MetaDesc: "Printer showing offline or not printing? Restart order, reconnecting to Wi-Fi, clearing the queue and setting the default printer in Windows.",
	},
	{
		Slug:    "email-not-sending-receiving",
		Title:   "Email stopped sending or receiving",
		Kicker:  "Email",
		Level:   "Easy",
		Time:    "10 minutes",
		Summary: "First work out whether it's the email account or Outlook on the computer. That one check saves a lot of time.",
		Steps: []template.HTML{
			"On your phone or in a web browser, log in to your email through the provider's website — <strong>outlook.com</strong>, <strong>gmail.com</strong>, Telstra/Bigpond webmail, etc. If new mail is there and you can send, the account is fine and the problem is on the computer.",
			"If the website says the password is wrong: has anyone changed it recently, or did the provider email you about a change? Reset it there first, then update it in Outlook when prompted.",
			"In Outlook, look at the <strong>bottom-right corner</strong>. If it says <em>Working Offline</em> or <em>Disconnected</em>, click the <strong>Send/Receive</strong> tab and click <strong>Work Offline</strong> to switch it back on.",
			"Check the <strong>Outbox</strong> folder. A stuck message with a huge attachment blocks everything behind it — delete it and try again.",
			"Is the mailbox full? Providers stop delivering when the quota is hit. Empty <strong>Deleted Items</strong> and <strong>Junk</strong>, and delete a few old emails with big attachments.",
			"Restart the computer and open Outlook again.",
		},
		IfNot: []template.HTML{
			"Telstra/Bigpond, Optus and iPrimus have all forced account changes on customers in the last couple of years — if you've had a letter or email about a \"migration\", that's the cause and it needs the account re-setting up in Outlook.",
			"Outlook won't open at all, or sits on a spinning wheel? Hold <kbd>Ctrl</kbd> while clicking the Outlook icon to start it in safe mode; if that opens, an add-in is the culprit.",
		},
		StopWhen: "The webmail works but Outlook won't after the steps above, contacts or folders have vanished, or the provider has changed your account type.",
		Service:  "email-outlook",
		MetaDesc: "Outlook not sending or receiving? Check webmail first, Work Offline, stuck Outbox, full mailbox — quick checks before you book.",
	},
	{
		Slug:    "windows-11-ready",
		Title:   "Can my computer run Windows 11? (and how to upgrade)",
		Kicker:  "Windows",
		Level:   "Easy",
		Time:    "5 minutes to check; 1–2 hours to upgrade",
		Summary: "Windows 10 stopped getting security updates in October 2025. Here's how to check whether your PC can move to Windows 11, and how to do it.",
		Before:  "Before any upgrade: make sure your files are backed up (OneDrive or an external drive), plug a laptop into power, and allow a couple of hours.",
		Steps: []template.HTML{
			"<strong>Quick check:</strong> open <strong>Settings → Windows Update</strong>. If Windows 11 is offered there with a <em>Download and install</em> button, your PC is compatible — that's the easiest route.",
			"<strong>Not offered?</strong> Search the Start menu for <strong>Windows Security</strong> → <strong>Device security</strong>. If you see a <em>Security processor</em> section, click <em>Security processor details</em>: Windows 11 needs <strong>Specification version 2.0</strong> (a TPM 2.0 chip). No Security processor section usually means it's switched off in the BIOS or the machine is too old.",
			"For a definitive answer, download Microsoft's free <strong>PC Health Check</strong> app (search \"PC Health Check\" on microsoft.com), run it and click <em>Check now</em>. It tells you exactly what passes and fails.",
			"<strong>To upgrade</strong> when Windows Update won't offer it: go to <strong>microsoft.com/software-download/windows11</strong> and use the <em>Windows 11 Installation Assistant</em>. Run it, accept, and let it work — the PC will restart several times.",
			"Afterwards, run Windows Update again until it says you're up to date, and check your printer still works (see the <a href=\"/fix-it-yourself/printer-offline\">printer guide</a> if not).",
		},
		IfNot: []template.HTML{
			"If PC Health Check says the processor isn't supported or there's no TPM 2.0, it's usually not worth forcing — I can tell you whether a cheap part or setting fixes it, or whether it's replacement time (and move everything across for you).",
		},
		StopWhen: "PC Health Check reports a TPM or Secure Boot problem (that's a BIOS setting), the upgrade fails partway, or you'd just rather someone did it and moved your files.",
		Service:  "new-computer-setup",
		MetaDesc: "Check whether your PC has TPM 2.0 and can run Windows 11, then upgrade with Windows Update or the Installation Assistant. Windows 10 support ended October 2025.",
	},
	{
		Slug:    "windows-wont-start",
		Title:   "Windows won't start — black screen, spinning dots or a repair loop",
		Kicker:  "Windows",
		Level:   "A bit fiddly",
		Time:    "20–40 minutes",
		Summary: "If the computer powers on but never gets to the login screen, Windows has a built-in repair menu you can reach — and its first two options are safe to try.",
		Before:  "This is for a PC that turns on (lights, fan, logo) but doesn't reach Windows. If nothing at all happens when you press power, skip to <em>When to stop</em>.",
		Steps: []template.HTML{
			"<strong>Stuck on \"Working on updates\" or a percentage?</strong> Give it time — up to two hours if the number is still moving. Don't switch it off while it's moving.",
			"To reach the repair menu: turn the PC on, and as soon as you see the maker's logo or the spinning dots, <strong>hold the power button</strong> until it switches off. Do this <strong>three times</strong>. On the next start Windows says <em>Preparing Automatic Repair</em>.",
			"When you see <em>Automatic Repair</em>, click <strong>Advanced options → Troubleshoot → Advanced options</strong>.",
			"Try <strong>Startup Repair</strong> first. Let it run; it may take a while and may restart the machine.",
			"If it comes back with <em>couldn't repair</em>, go back to Advanced options and try <strong>System Restore</strong>, choosing the most recent restore point from before the trouble started. Your files are not affected by System Restore, only recent programs/updates.",
			"If Windows starts, run <strong>Settings → Windows Update</strong> and let it finish anything pending.",
		},
		IfNot: []template.HTML{
			"If it asks for a <strong>BitLocker recovery key</strong>, see the <a href=\"/fix-it-yourself/locked-out-bitlocker\">locked-out guide</a> — the key is on your Microsoft account.",
			"<strong>Don't</strong> choose <em>Reset this PC</em> unless you're certain everything is backed up — it can remove your programs and, with the wrong option, your files.",
		},
		StopWhen: "Startup Repair and System Restore both fail, it's asking for a recovery key you don't have, or the machine shows no signs of power at all. Your data is almost always still recoverable — that's a visit.",
		Service:  "computer-repairs",
		MetaDesc: "PC turns on but Windows won't load? How to reach Automatic Repair, run Startup Repair and System Restore safely — and when to stop and call.",
	},
	{
		Slug:    "locked-out-bitlocker",
		Title:   "Locked out — forgotten password, PIN, or a BitLocker recovery key screen",
		Kicker:  "Windows",
		Level:   "Easy",
		Time:    "10 minutes",
		Summary: "Most modern PCs sign in with a Microsoft account, which means both the password and the BitLocker key can be recovered from any other device.",
		Steps: []template.HTML{
			"<strong>Blue screen asking for a 48-digit BitLocker recovery key?</strong> On your phone or another computer, go to <strong>aka.ms/myrecoverykey</strong> and sign in with the Microsoft account you use on the PC. The key is listed there next to the PC's name — type it in.",
			"<strong>Forgotten your password or PIN?</strong> On the login screen click <em>I forgot my PIN</em> or <em>I forgot my password</em> and follow the prompts — you'll need access to the phone number or backup email on the account.",
			"Alternatively reset it from another device at <strong>account.live.com/password/reset</strong>, then sign in on the PC with the new password (it needs an internet connection the first time).",
			"If the login says the password is wrong but you're sure it's right, check <kbd>Caps Lock</kbd> and try typing the password into the username box to see what you're actually typing.",
		},
		IfNot: []template.HTML{
			"If it's a <em>local</em> account (no Microsoft account, no email shown on the login screen), there's no online reset — that needs a visit, and your files can still be recovered.",
			"If the PC belonged to someone else or was set up by a business, the BitLocker key may be with them.",
		},
		StopWhen: "The recovery key isn't on your account, it's a local account, or you're not sure which Microsoft account was used. Don't guess more than a few times on BitLocker.",
		Service:  "scam-virus-security",
		MetaDesc: "Windows asking for a BitLocker recovery key, or forgotten your PIN or password? Where to find the key on your Microsoft account and how to reset the password from another device.",
	},
	{
		Slug:    "slow-computer",
		Title:   "Computer running slow — the safe first steps",
		Kicker:  "Windows",
		Level:   "Easy",
		Time:    "20 minutes",
		Summary: "Before assuming it's worn out, try these. Half of \"slow\" computers just haven't been restarted in weeks or are out of space.",
		Steps: []template.HTML{
			"<strong>Restart</strong> — click Start → Power → <strong>Restart</strong> (not Shut down; Windows' shut-down keeps things in memory). Do this once a week.",
			"<strong>Updates:</strong> Settings → Windows Update → Check for updates. Install and restart. Then leave it 20 minutes — it's often busy finishing in the background.",
			"<strong>Space:</strong> Settings → System → Storage. If the C: drive is nearly full (less than about 10% free), click <em>Temporary files</em> and remove them, and empty the Recycle Bin.",
			"<strong>Startup programs:</strong> press <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>Esc</kbd> → <strong>Startup apps</strong>. Right-click and <em>Disable</em> anything you don't need the moment you turn on (Spotify, Teams, updaters). Don't disable your antivirus.",
			"<strong>Uninstall what you don't use:</strong> Settings → Apps → Installed apps. Sort by size; remove trials and things you've never opened. If unsure, leave it.",
			"Optional: Microsoft's own free <strong>PC Manager</strong> app (from the Microsoft Store) has a one-click <em>Boost</em> and a health check that's safe for anyone to use.",
		},
		IfNot: []template.HTML{
			"If it's still slow after all that, it's usually a spinning hard drive that wants replacing with an SSD (a big, cheap improvement on most machines) or too little memory — I can tell you which in a few minutes.",
		},
		StopWhen: "It's slow from the moment it starts, the fan roars constantly, programs crash, or the drive is over 5–6 years old.",
		Service:  "computer-repairs",
		MetaDesc: "Slow Windows PC? Restart properly, updates, disk space, startup apps and safe clean-up — the steps worth trying before an SSD upgrade or a visit.",
	},
	{
		Slug:    "phone-email-not-working",
		Title:   "Email not arriving on my iPhone or Android",
		Kicker:  "Phone",
		Level:   "Easy",
		Time:    "10 minutes",
		Summary: "When the computer gets email but the phone doesn't, the account on the phone has usually just lost its login. Removing it and adding it back fixes most cases.",
		Before:  "Removing an account from the phone doesn't delete your email — it lives with the provider and comes back when you re-add it.",
		Steps: []template.HTML{
			"Check the obvious: is the phone on Wi-Fi or mobile data, and does a web page load?",
			"Open the mail app and pull down on the inbox to refresh. If it says a <em>password is incorrect</em>, tap it and enter the current password (if you changed it recently, this is why).",
			"<strong>iPhone:</strong> Settings → Apps → Mail → Mail Accounts (older iOS: Settings → Mail → Accounts). Tap the account → <strong>Delete Account</strong>. Then <em>Add Account</em>, choose the provider (Outlook.com/Hotmail, Google, or <em>Other</em> for Bigpond/Optus), sign in.",
			"<strong>Android (Gmail app):</strong> tap your picture → Manage accounts on this device → tap the account → Remove account. Then in Gmail: picture → Add another account.",
			"Give it a few minutes to download the recent mail. Old mail may take longer.",
		},
		IfNot: []template.HTML{
			"Bigpond, Optus and iPrimus accounts sometimes need specific server settings after a provider change — if <em>Other</em> won't accept the login, that's one for a visit or a call.",
		},
		StopWhen: "The password is definitely right and it still won't add, or the same account is failing on the computer too (then it's the account, see the <a href=\"/fix-it-yourself/email-not-sending-receiving\">email guide</a>).",
		Service:  "phone-tablet-tv",
		MetaDesc: "Hotmail, Gmail or Bigpond email not arriving on your iPhone or Android? Remove and re-add the account — safe steps that fix most cases.",
	},
	{
		Slug:    "check-and-repair-windows-files",
		Title:   "Windows behaving oddly? Run Microsoft's built-in repair (DISM and SFC)",
		Kicker:  "Windows",
		Level:   "A bit fiddly",
		Time:    "30–60 minutes, mostly waiting",
		Summary: "Two commands from Microsoft that check Windows' own files and repair any that are damaged. Safe — they don't touch your documents — but you'll be typing at a black command window.",
		Before:  "Only try this if you're comfortable typing exact commands. Nothing here deletes your files; the worst case is that it simply doesn't help.",
		Steps: []template.HTML{
			"Click Start and type <strong>cmd</strong>. Right-click <em>Command Prompt</em> and choose <strong>Run as administrator</strong>. Click <em>Yes</em>.",
			"Type exactly (note the spaces before each slash) and press Enter:<br><code>DISM.exe /Online /Cleanup-image /Restorehealth</code><br>This can sit at a percentage for a long time — leave it until it says <em>The operation completed successfully</em>.",
			"Then type and press Enter:<br><code>sfc /scannow</code><br>Wait for <em>Verification 100% complete</em>. It will say either that it found no problems, or that it repaired some.",
			"Type <code>exit</code>, press Enter, and <strong>restart</strong> the computer.",
		},
		IfNot: []template.HTML{
			"If SFC says it found corrupt files but <em>could not fix</em> some, run both commands once more after the restart. If it still can't, that's the point to book.",
		},
		StopWhen: "The commands error out immediately, Windows won't get far enough to open a command window, or you're not comfortable — there's no shame in that, it's what I'm for.",
		Service:  "computer-repairs",
		MetaDesc: "How to run DISM and SFC /scannow to check and repair Windows system files — the safe Microsoft way, step by step, and when to stop.",
	},
	{
		Slug:    "backup-basics",
		Title:   "Back up your photos and files so a dead computer isn't a disaster",
		Kicker:  "Data",
		Level:   "Easy",
		Time:    "20 minutes to set up, then automatic",
		Summary: "Most of the data-recovery calls I get would have been a non-event with one of these switched on. Pick either — or both.",
		Steps: []template.HTML{
			"<strong>Option A — OneDrive (built into Windows).</strong> Click the cloud icon near the clock (or search <em>OneDrive</em>), sign in with your Microsoft account, and turn on <strong>Back up folders</strong> for Desktop, Documents and Pictures. 5 GB is free; if you have lots of photos, the paid Microsoft 365 Basic plan gives 100 GB for a few dollars a month.",
			"<strong>Option B — an external drive.</strong> Buy a small external SSD or hard drive (bigger than the space used on your C: drive). Plug it in, then Settings → search <strong>File History</strong> → <em>Backup options</em> → Add a drive → turn on <em>Automatically back up my files</em>. Leave the drive plugged in.",
			"Either way, <strong>check it once</strong>: open OneDrive in a browser, or the external drive in File Explorer, and confirm your recent photos are actually there.",
			"Phones: iPhone → Settings → your name → iCloud → Photos on. Android → Google Photos → Backup on. That covers the photos that only exist on the phone.",
		},
		IfNot: []template.HTML{
			"If the computer is already refusing to start and the files aren't backed up — don't keep trying to boot it. The drive is usually readable; that's a <a href=\"/services/data-recovery-backup\">data recovery visit</a>.",
		},
		StopWhen: "You have more data than fits the free space, several computers to cover, or you'd like it set up and checked for you.",
		Service:  "data-recovery-backup",
		MetaDesc: "Set up OneDrive or File History so your photos and documents survive a dead computer — 20 minutes, then automatic. And what to do if it's already too late.",
	},
	{
		Slug:    "windows-update-stuck",
		Title:   "Windows update stuck at a percentage for hours",
		Kicker:  "Windows",
		Level:   "Easy",
		Time:    "Up to 2 hours of waiting, then 10 minutes",
		Summary: "\"Working on updates — 37% — don't turn off your computer\" for half a day. Here's how long to wait, and what to do when it really is stuck.",
		Before:  "If the percentage has changed at all in the last hour, it isn't stuck — leave it. Big feature updates on an older machine genuinely take two hours or more.",
		Steps: []template.HTML{
			"Note the percentage, then <strong>walk away for an hour</strong>. Come back and compare. Moved? Keep waiting.",
			"Same number after a solid hour (two if it's the big yearly update)? Hold the <strong>power button</strong> until the machine switches off. Wait 30 seconds, turn it on.",
			"Most of the time it will either finish the update on the way up, or say <em>Undoing changes</em> and take you back to Windows as it was. Both are fine.",
			"Once you're at the desktop: <strong>Settings → Windows Update → Check for updates</strong> and let it try again, plugged in, with nothing else running.",
			"If it fails again with an error code, note the code down (a photo on your phone is fine) — it tells me exactly what's wrong.",
		},
		IfNot: []template.HTML{
			"If it loops — update, restart, undo, update — three times, stop. There's an underlying problem (usually low disk space or a damaged system file) that's quick to fix on a visit but tedious to guess at.",
			"If instead of Windows you get a repair screen, follow the <a href=\"/fix-it-yourself/windows-wont-start\">won't start guide</a>.",
		},
		StopWhen: "It loops more than twice, shows an error code you can't get past, or the machine is more than a few years old and this keeps happening.",
		Service:  "computer-repairs",
		MetaDesc: "Windows stuck on 'Working on updates' for hours? How long to wait, when it's safe to force it off, and what to do next.",
	},
	{
		Slug:    "new-printer-setup",
		Title:   "Setting up a new printer (Wi-Fi first, then Windows)",
		Kicker:  "Printing",
		Level:   "Easy",
		Time:    "20–30 minutes",
		Summary: "The trick with a new printer is to get it onto your Wi-Fi before you touch the computer. Do it in this order and it usually just works.",
		Before:  "Have your Wi-Fi name and password handy (they're on the sticker on the modem unless you changed them).",
		Steps: []template.HTML{
			"Unbox it, remove <strong>all</strong> the blue tape and orange plastic (there's always one more piece inside), fit the ink or toner, load paper, and turn it on. Let it finish its first-time set-up on its own screen.",
			"On the printer's screen: <strong>Settings/Setup → Network → Wi-Fi setup wizard</strong>. Choose your Wi-Fi name and type the password carefully — capitals matter. It should say <em>Connected</em>. (No screen? Use the maker's phone app — Epson Smart Panel, Canon PRINT, HP Smart, Brother Mobile Connect — it walks you through joining Wi-Fi.)",
			"Print the network status page from the printer's menu if you can — it confirms it's on <em>your</em> Wi-Fi.",
			"On the computer, make sure it's on the <strong>same Wi-Fi</strong>, then <strong>Settings → Bluetooth &amp; devices → Printers &amp; scanners → Add device</strong>. Wait; the printer should appear — click <em>Add device</em> next to it.",
			"Once added, click it and choose <strong>Set as default</strong>. Print a test page from that screen.",
			"For scanning, install the maker's app from their website or the Microsoft Store (Epson ScanSmart, HP Smart, Canon IJ Scan Utility, Brother iPrint&amp;Scan) — Windows' built-in scan works too but the maker's app is easier.",
		},
		IfNot: []template.HTML{
			"If it doesn't appear in <em>Add device</em> after a couple of minutes, click <em>Add manually</em> → <em>Add a printer using an IP address</em> — the IP is on the printer's network status page — and choose <em>Autodetect</em>.",
			"HP Smart insisting you create an account to scan? You can skip that with the offline/\"continue without\" option; if it won't let you, that's a known nuisance and I can sort it.",
			"Adding it to a phone: install the maker's app, and it finds the printer on the same Wi-Fi.",
		},
		StopWhen: "The printer won't join Wi-Fi at all, it joins but Windows never sees it, or you have several computers/phones to set up and would rather it just worked.",
		Service:  "printers-scanners",
		MetaDesc: "New printer setup the easy way: connect it to Wi-Fi from its own screen first, then add it in Windows, set as default, and get scanning working.",
	},
	{
		Slug:    "outlook-wont-open",
		Title:   "Outlook won't open, freezes, or sits on \"Loading profile\"",
		Kicker:  "Email",
		Level:   "A bit fiddly",
		Time:    "15 minutes",
		Summary: "When Outlook itself won't start — spinning wheel, \"Loading profile\", or it opens and freezes — it's usually a stuck add-in or a corrupted profile. Two safe things to try.",
		Before:  "Your email isn't lost. It's either on the provider's server or in a data file on the computer; neither is touched by these steps.",
		Steps: []template.HTML{
			"Make sure it isn't already running in the background: press <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>Esc</kbd>, find <em>Microsoft Outlook</em> in the list, click it and <strong>End task</strong>. Then try opening it again normally.",
			"<strong>Start in safe mode:</strong> hold down <kbd>Ctrl</kbd> on the keyboard while you click the Outlook icon. Click <em>Yes</em> to \"start in safe mode\". If Outlook opens this way, an add-in is the problem.",
			"In safe mode: <strong>File → Options → Add-ins</strong>. At the bottom, next to <em>Manage: COM Add-ins</em>, click <strong>Go</strong>. Untick everything, click OK, close Outlook and open it normally. If that fixes it, re-tick add-ins one at a time to find the culprit (it's often an old antivirus or PDF add-in).",
			"Still won't open? <strong>Restart the computer</strong> and try once more before going further — a Windows update in progress can hold Outlook hostage.",
			"Check for updates: in any Office app that <em>does</em> open (Word), <strong>File → Account → Update Options → Update Now</strong>.",
		},
		IfNot: []template.HTML{
			"The next step is a fresh Outlook <em>profile</em> (Control Panel → Mail → Show Profiles → Add). It's safe with modern accounts (Microsoft 365, Outlook.com, Gmail) because the mail is on the server — but with older POP accounts it needs care so the local mail file is reattached. That's a good point to stop.",
			"If it's the <em>new</em> Outlook (the blue-and-white one) that's misbehaving, you can switch back to classic Outlook with the toggle at the top right for now.",
		},
		StopWhen: "Safe mode doesn't open it either, it asks about repairing a data file, or you use a POP account and would rather not risk the mail file.",
		Service:  "email-outlook",
		MetaDesc: "Outlook won't open or is stuck on Loading profile? Try safe mode (Ctrl-click), disable add-ins, and know when a new profile is safe — before you book.",
	},
	{
		Slug:    "second-monitor-not-detected",
		Title:   "Second monitor or TV not detected",
		Kicker:  "Windows",
		Level:   "Easy",
		Time:    "10 minutes",
		Summary: "The screen worked yesterday and now it's blank or \"No signal\". Almost always a cable, an input button, or Windows deciding to show on one screen only.",
		Steps: []template.HTML{
			"On the monitor itself, press its <strong>Input / Source</strong> button and make sure it's set to the connection you're using (HDMI 1, HDMI 2, DisplayPort…). Monitors switch input on their own more often than you'd think.",
			"<strong>Reseat both ends</strong> of the video cable — unplug and plug back in firmly at the computer and at the monitor. Try a different HDMI socket on the monitor if it has one.",
			"On the keyboard press <kbd>Windows</kbd> + <kbd>P</kbd>. A panel slides in on the right — pick <strong>Extend</strong> (or <em>Duplicate</em> if you want the same picture on both). \"PC screen only\" is what makes a second screen go dark.",
			"Still nothing? Right-click the desktop → <strong>Display settings</strong> → scroll to <em>Multiple displays</em> → click <strong>Detect</strong>.",
			"Restart the computer with the monitor already switched on and connected.",
			"Laptop with a docking station or USB-C hub: unplug the hub, plug it back in, and make sure the hub has its own power if it needs it.",
		},
		IfNot: []template.HTML{
			"Try the monitor with another device (a laptop, a games console) — if it works there, the monitor is fine and the problem is the computer's graphics driver or port; if it doesn't, it's the monitor or cable.",
			"Try a different cable. Cheap or long HDMI cables fail quietly.",
		},
		StopWhen: "It works with another device but not this computer (a driver or graphics card job), or there's a burning smell, flicker or lines on the screen.",
		Service:  "computer-repairs",
		MetaDesc: "Second monitor not detected or showing No signal? Check the input button, reseat cables, Windows+P to Extend, and Detect in Display settings.",
	},
	{
		Slug:    "no-sound",
		Title:   "No sound from the computer",
		Kicker:  "Windows",
		Level:   "Easy",
		Time:    "10 minutes",
		Summary: "YouTube's playing but you can't hear it. Nine times out of ten Windows is sending the sound to the wrong place — a monitor with no speakers, an unplugged headset, or Bluetooth headphones in a drawer.",
		Steps: []template.HTML{
			"Click the <strong>speaker icon</strong> near the clock. Make sure the volume slider isn't at zero or muted (a little X on the icon means muted — click it).",
			"Next to the volume slider there's a small arrow <strong>&gt;</strong> — click it to see the list of output devices. Choose your actual <strong>speakers or headphones</strong>, not the monitor/TV (which often has no speakers) and not a Bluetooth device that isn't on.",
			"External speakers: check they're switched on, turned up, and plugged into the <strong>green</strong> socket (or the headphone socket on a laptop). Give the plug a firm push — it's often not all the way in.",
			"Still silent? Right-click the speaker icon → <strong>Troubleshoot sound problems</strong> and let Windows have a go. It fixes the boring cases.",
			"Check the app itself: a video player, Zoom or a browser tab can be muted on its own. In the browser, look for a crossed-out speaker on the tab.",
			"Restart the computer.",
		},
		IfNot: []template.HTML{
			"If it's Bluetooth headphones or a speaker: on them, hold the power button until they go into pairing mode, then Settings → Bluetooth &amp; devices → Add device.",
			"If sound only vanished after a Windows update, the audio driver may need reinstalling — that's a short visit.",
		},
		StopWhen: "The speakers work on your phone but not the computer after all of the above, or the sound is crackly/distorted (often a hardware or driver fault).",
		Service:  "computer-repairs",
		MetaDesc: "No sound on your Windows PC? Check the output device next to the volume slider, the green socket, mute buttons and the sound troubleshooter.",
	},
	{
		Slug:    "antivirus-renewal-real-or-scam",
		Title:   "Is this Norton / McAfee renewal message real? And do I need it?",
		Kicker:  "Security",
		Level:   "Easy",
		Time:    "10 minutes",
		Summary: "\"Your subscription has expired — renew now for $199\" pops up constantly. Some are genuine (and overpriced), some are scams. How to tell, and how to cancel a real one you don't want.",
		Before:  "Windows 10 and 11 include Microsoft Defender, which is a perfectly good antivirus for home use and free. You do not <em>need</em> Norton or McAfee.",
		Steps: []template.HTML{
			"<strong>Is it real?</strong> A genuine message comes from the program on your computer, in its own window with its normal look. A <em>scam</em> is an email or a web page — it appears in your browser or inbox, uses urgent language, has spelling mistakes, and wants you to click a link or call a number. When in doubt: <strong>don't click, don't call.</strong>",
			"Check the truth yourself: open the Norton or McAfee program from the Start menu (not from the message) and look at its subscription status. If it says active, the message was a scam. Delete it.",
			"<strong>Received a scam email saying you've been charged?</strong> You haven't. Don't ring the number to \"cancel\". Check your actual bank statement instead.",
			"<strong>Want to cancel a real subscription?</strong> Do it on their website: sign in at <em>my.norton.com</em> or <em>home.mcafee.com</em> → Subscriptions → turn <strong>auto-renewal off</strong>. Cancel there, never through a pop-up.",
			"<strong>Removing it:</strong> Settings → Apps → Installed apps → find Norton or McAfee → Uninstall. Windows Defender switches itself back on automatically. Restart afterwards.",
			"Confirm you're protected: Start → <strong>Windows Security</strong> → the shield should be green with no red warnings.",
		},
		IfNot: []template.HTML{
			"If you were charged for something you didn't intend, ring your bank using the number on your card and ask for a chargeback.",
			"If the uninstaller refuses or the pop-ups continue after removal, the maker has a dedicated \"remove tool\" — that, or a visit, sorts it.",
		},
		StopWhen: "You've already given card details in response to a message, the pop-ups continue after uninstalling, or you'd just like someone to sit down and go through what's on the machine.",
		Service:  "scam-virus-security",
		MetaDesc: "Norton or McAfee 'subscription expired' pop-up — real or scam? How to check, cancel auto-renewal properly, remove it, and rely on Windows Defender.",
	},
	{
		Slug:    "old-computer-get-data-off",
		Title:   "Getting your files off an old computer before it goes",
		Kicker:  "Data",
		Level:   "Easy",
		Time:    "30 minutes if it still starts",
		Summary: "Recycling an old PC or laptop? Get the photos, documents and email off it first — and wipe it before it leaves the house.",
		Before:  "If the old computer still turns on and gets to Windows, this is easy. If it doesn't, don't keep trying — jump to <em>If that didn't work</em>.",
		Steps: []template.HTML{
			"Plug in a USB stick or external drive with enough room. In File Explorer, open <strong>This PC</strong> and copy these folders across: <strong>Desktop, Documents, Pictures, Videos, Music, Downloads</strong>. Check the drive afterwards — open a few photos to make sure they copied.",
			"Signed into <strong>OneDrive</strong> or Google Drive on the old machine? Then most of it is already in the cloud — sign in on the new computer and it comes down. Check the folders above anyway; not everything lives in OneDrive.",
			"<strong>Email:</strong> if it's Outlook.com, Gmail, Bigpond or Microsoft 365, the mail is on the server — just add the account on the new computer. If Outlook shows a <em>.pst</em> file in the folder list (older POP setups), copy that file too: <strong>File → Account Settings → Data Files</strong> shows where it is.",
			"<strong>Browser favourites and passwords:</strong> in Edge or Chrome, sign in with your Microsoft/Google account and turn on Sync — they follow you. Or Settings → Favourites → Export to a file and copy it.",
			"Take a photo of anything you can't copy: licence keys on stickers, the list of programs installed, printer settings.",
			"<strong>Before it leaves:</strong> Settings → System → Recovery → <strong>Reset this PC → Remove everything</strong> → choose <em>Clean the drive</em>. That wipes your data properly. Or remove the drive and keep it.",
		},
		IfNot: []template.HTML{
			"If it won't start, the drive is almost always still readable — but forcing it to boot repeatedly can make it worse. That's a <a href=\"/services/data-recovery-backup\">data recovery visit</a>: I take the drive out, copy everything, and put it on the new machine or a drive for you.",
			"Moving to a new computer? I can do the whole transfer, including Outlook and printers — see <a href=\"/services/new-computer-setup\">new computer setup</a>.",
		},
		StopWhen: "It won't turn on, it's a laptop you'd rather not open, there's a lot of email to move, or you'd like the whole thing moved and the old one wiped for you.",
		Service:  "data-recovery-backup",
		MetaDesc: "Recycling an old PC? Copy the right folders, get email and favourites across, and wipe the drive before it goes — or what to do if it won't start.",
	},
}
