package config

func ResolveOllamaHost(cfg *Config, provider string) string {
	profile := cfg.Models.Profiles[cfg.Models.DefaultProfile]
	switch provider {
	case "ollama_cloud":
		return cfg.Models.OllamaCloud.Host
	case "ollama_local":
		if profile.Cheap.Host != "" {
			return profile.Cheap.Host
		}
		return "http://localhost:11434"
	default:
		return ""
	}
}

func OllamaAPIKey(cfg *Config) string {
	return cfg.Models.OllamaCloud.APIKey
}