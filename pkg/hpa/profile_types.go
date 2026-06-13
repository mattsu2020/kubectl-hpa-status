package hpa

// ControllerProfile describes HPA controller-manager settings that affect
// controller decisions. Values may be observed from kube-controller-manager
// arguments or default assumptions.
type ControllerProfile struct {
	Source                  string   `json:"source" yaml:"source"`
	SyncPeriod              string   `json:"syncPeriod" yaml:"syncPeriod"`
	DownscaleStabilization  string   `json:"downscaleStabilization" yaml:"downscaleStabilization"`
	InitialReadinessDelay   string   `json:"initialReadinessDelay" yaml:"initialReadinessDelay"`
	CPUInitializationPeriod string   `json:"cpuInitializationPeriod" yaml:"cpuInitializationPeriod"`
	Tolerance               string   `json:"tolerance" yaml:"tolerance"`
	Warnings                []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// DefaultControllerProfile returns the Kubernetes controller-manager default
// HPA timing settings that matter for user-facing interpretation.
func DefaultControllerProfile() ControllerProfile {
	return ControllerProfile{
		Source:                  "defaults",
		SyncPeriod:              "15s",
		DownscaleStabilization:  "5m",
		InitialReadinessDelay:   "30s",
		CPUInitializationPeriod: "5m",
		Tolerance:               "0.1",
	}
}
