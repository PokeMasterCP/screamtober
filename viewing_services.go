package main

import "errors"

type viewingService struct {
	ID, Name, Icon string
	Retired        bool
}

// IDs are persisted on challenge entries: keep them stable. To remove a choice,
// set Retired instead of deleting it, preserving names and icons in old years.
var viewingServices = []viewingService{
	{ID: "prime-video", Name: "Amazon Prime Video", Icon: "/assets/services/prime-video.svg"},
	{ID: "netflix", Name: "Netflix", Icon: "/assets/services/netflix.svg"},
	{ID: "shudder", Name: "Shudder", Icon: "/assets/services/shudder.png"},
	{ID: "plex", Name: "Plex", Icon: "/assets/services/plex.svg"},
	{ID: "hulu", Name: "Hulu", Icon: "/assets/services/hulu.svg"},
	{ID: "disney-plus", Name: "Disney+", Icon: "/assets/services/disney-plus.svg"},
	{ID: "apple-tv", Name: "Apple TV", Icon: "/assets/services/appletv.svg"},
	{ID: "paramount-plus", Name: "Paramount+", Icon: "/assets/services/paramountplus.svg"},
	{ID: "peacock", Name: "Peacock", Icon: "/assets/services/peacock.svg"},
	{ID: "tubi", Name: "Tubi", Icon: "/assets/services/tubi.svg"},
	{ID: "theaters", Name: "In theaters", Icon: "/assets/services/theaters.svg"},
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
