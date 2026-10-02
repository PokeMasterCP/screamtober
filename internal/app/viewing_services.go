package app

import "errors"

type viewingService struct {
	ID, Name, Icon string
	Retired        bool
}

// IDs are persisted on challenge entries: keep them stable. To remove a choice,
// set Retired instead of deleting it, preserving names and icons in old years.
// The list order is the order offered when adding a pick.
var viewingServices = []viewingService{
	{ID: "netflix", Name: "Netflix", Icon: serviceIcon("netflix.svg")},
	{ID: "prime-video", Name: "Amazon Prime Video", Icon: serviceIcon("prime-video.png")},
	{ID: "hbo-max", Name: "HBO Max", Icon: serviceIcon("hbo-max.svg")},
	{ID: "hulu", Name: "Hulu", Icon: serviceIcon("hulu.svg")},
	{ID: "disney-plus", Name: "Disney+", Icon: serviceIcon("disney-plus.png")},
	{ID: "apple-tv", Name: "Apple TV", Icon: serviceIcon("apple-tv.svg")},
	{ID: "paramount-plus", Name: "Paramount+", Icon: serviceIcon("paramount-plus.svg")},
	{ID: "peacock", Name: "Peacock", Icon: serviceIcon("peacock.svg")},
	{ID: "shudder", Name: "Shudder", Icon: serviceIcon("shudder.png")},
	{ID: "amc-plus", Name: "AMC+", Icon: serviceIcon("amc-plus.png")},
	{ID: "starz", Name: "Starz", Icon: serviceIcon("starz.svg")},
	{ID: "mgm-plus", Name: "MGM+", Icon: serviceIcon("mgm-plus.png")},
	{ID: "tubi", Name: "Tubi", Icon: serviceIcon("tubi.svg")},
	{ID: "pluto-tv", Name: "Pluto TV", Icon: serviceIcon("pluto-tv.png")},
	{ID: "roku-channel", Name: "The Roku Channel", Icon: serviceIcon("roku-channel.svg")},
	{ID: "youtube", Name: "YouTube", Icon: serviceIcon("youtube.svg")},
	{ID: "fandango-at-home", Name: "Fandango at Home", Icon: serviceIcon("fandango-at-home.png")},
	{ID: "plex", Name: "Plex", Icon: serviceIcon("plex.svg")},
	{ID: "video-on-demand", Name: "Video on demand (rental)", Icon: serviceIcon("video-on-demand.svg")},
	{ID: "theaters", Name: "In theaters", Icon: serviceIcon("theaters.svg")},
}

// Icons are cached for a day under fixed paths. Bump the version whenever an
// existing file's artwork changes so browsers fetch the replacement.
const serviceIconVersion = "2"

func serviceIcon(file string) string {
	return "/assets/services/" + file + "?v=" + serviceIconVersion
}

var errViewingService = errors.New("unsupported viewing service")

func selectableViewingServices() []viewingService {
	var choices []viewingService
	for _, service := range viewingServices {
		if !service.Retired {
			choices = append(choices, service)
		}
	}
	return choices
}

func validViewingService(id string) bool {
	if id == "" {
		return true
	}
	for _, service := range viewingServices {
		if service.ID == id && !service.Retired {
			return true
		}
	}
	return false
}

func findViewingService(id string) viewingService {
	for _, service := range viewingServices {
		if service.ID == id {
			return service
		}
	}
	return viewingService{Name: "Not decided"}
}
