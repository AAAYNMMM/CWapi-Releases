package main

func (a *App) ReportFrontendReady(marker string) error {
	return a.service.ReportFrontendReady(marker)
}

func (a *App) GUIProbeConfig() string {
	return a.service.GUIProbeConfig()
}

func (a *App) ReportGUIProbe(payload string) error {
	return a.service.ReportGUIProbe(payload)
}
