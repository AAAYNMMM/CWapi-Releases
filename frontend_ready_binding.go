package main

func (a *App) ReportFrontendReady(marker string) error {
	service, err := a.core()
	if err != nil {
		return err
	}
	return service.ReportFrontendReady(marker)
}

func (a *App) GUIProbeConfig() string {
	service, _ := a.core()
	if service == nil {
		return ""
	}
	return service.GUIProbeConfig()
}

func (a *App) ReportGUIProbe(payload string) error {
	service, err := a.core()
	if err != nil {
		return err
	}
	return service.ReportGUIProbe(payload)
}
