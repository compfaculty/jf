package repo

// embeddedCompanies contains pure company career pages (not job boards/aggregators).
// These are direct company sites where we scrape jobs directly.
var embeddedCompanies = []struct {
	Name  string
	URL   string
	Email string
}{
	// {"40Seas", "https://www.40seas.com/careers#positions", ""},
	// {"Agrematch", "https://www.agrematch.com/careers", ""},
	// {"Agora Real", "https://agorareal.com/careers/", ""},
	// {"Aidoc", "https://www.aidoc.com/about/careers/", ""},
	// {"AI21", "https://www.ai21.com/careers/", ""},
	// {"Akeyless", "https://www.akeyless.io/careers/#positions", ""},
	//{"Allot", "https://www.allot.com/careers/search/"},
	//{"Amdocs", "https://jobs.amdocs.com/careers"},
	//{"Anecdotes", "https://www.anecdotes.ai/careers"},
	//{"Arbe Robotics", "https://arberobotics.com/career/"},
	// {"Audiocodes", "https://www.audiocodes.com/careers/positions?countryGroup=Israel", ""},
	//{"Axonius", "https://www.axonius.com/company/careers/open-jobs"},
	//{"Buildots", "https://buildots.com/careers/co/development/"},
	//{"C2A Security", "https://c2a-sec.com/careers/"},
	//{"Carbyne", "https://carbyne.com/company/careers/"},
	//{"Cato Networks", "https://www.catonetworks.com/careers/"},
	//{"Check Point", "https://careers.checkpoint.com/index.php?m=cpcareers&a=search"},
	//{"Double AI", "https://doubleai.com/careers/"},
	//{"DriveNets", "https://drivenets.com/careers/"},
	//{"Gett", "https://www.gett.com/careers/?location=tel-aviv-israel#open-positions"},
	//{"Hailo", "https://hailo.ai/company-overview/careers/"},
	//{"JFrog", "https://join.jfrog.com/positions/?gh_office=25868"},
	//{"Kaltura", "https://corp.kaltura.com/company/careers/"},
	//{"Kornit", "https://careers.kornit.com/all-positions/"},
	//{"Mobileye", "https://careers.mobileye.com/jobs"},
	//{"Noma Security", "https://noma.security/careers/"},
	//{"ParaZero", "https://parazero.com/careers/"},
	//{"Pentera", "https://pentera.io/careers/"},
	//{"Perion", "https://perion.com/careers/"},
	//{"Seemplicity", "https://seemplicity.io/about/careers/"},
	//{"Ceragon Networks", "https://www.ceragon.com/about-ceragon/careers"},
	//{"Varonis", "https://careers.varonis.com/"},
	//{"Verint", "https://fa-epcb-saasfaprod1.fa.ocs.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX/jobs?location=Israel"},
	//{"Cognyte", "https://www.cognyte.com/careers/"},
	//{"CyberArk", "https://www.cyberark.com/careers/all-job-openings/"},
	//{"Cymulate", "https://cymulate.com/careers/#open-positions"},
	//{"D-Fend Solutions", "https://d-fendsolutions.com/about-us/careers/"},
	//{"Elsight", "https://www.elsight.com/careers/"},
	//{"Firebolt", "https://www.firebolt.io/careers"},
	//{"Gilat Satellite Networks", "https://www.gilat.com/career/#petah-tikva-israel"},
	//{"Global-e", "https://www.global-e.com/careers/"},
	//{"Idomoo", "https://www.idomoo.com/careers/#open-positions"},
	//{"Komodor", "https://komodor.com/careers/#open-positions-title"},
	//{"LayerX Security", "https://layerxsecurity.com/careers"},
	//{"Mobilicom", "https://mobilicom.com/careers/"},
}

// RSSFeed contains job boards/aggregators.
// These aggregate jobs from multiple companies.
// For each aggregator:
// - Name: display name
// - SourceURL: URL to scrape (for scraping) or RSS feed URL (for RSS feeds)
// - Type: "scraper" or "rss_feed"
// - RSSFeedURL: optional RSS feed URL (empty string if using scraper, or same as SourceURL for RSS feeds)
var RSSFeeds = []struct {
	Name       string
	SourceURL  string
	Type       string // "scraper" or "rss_feed"
	RSSFeedURL string // Optional RSS feed URL
}{
	// RSS Feed aggregators
	{"DOU Jobs", "https://jobs.dou.ua/vacancies/feeds/", "rss_feed", "https://jobs.dou.ua/vacancies/feeds/"},
	{"Jobicy", "https://jobicy.com/feed/job_feed", "rss_feed", "https://jobicy.com/feed/job_feed"},
	{"Real Work From Anywhere", "https://www.realworkfromanywhere.com/rss.xml", "rss_feed", "https://www.realworkfromanywhere.com/rss.xml"},
}

// embeddedAggregators combines both RSS feeds and job boards for seeding.
// This is used by SeedAggregators to populate the database.
var embeddedAggregators = []struct {
	Name      string
	SourceURL string
	Type      string
}{
	// RSS Feed aggregators
	{"DOU Jobs", "https://jobs.dou.ua/vacancies/feeds/", "rss_feed"},
	{"Jobicy", "https://jobicy.com/feed/job_feed", "rss_feed"},
	{"Real Work From Anywhere", "https://www.realworkfromanywhere.com/rss.xml", "rss_feed"},
	// Job board aggregators (scrapers)
	{"Secret TLV", "https://jobs.secrettelaviv.com/", "scraper"},
	{"Telfed Job Board", "https://www.telfed.org.il/job-board/", "scraper"},
}
