package goldenpath

// Delivery is omitted for the seven original configuration commands, preserving
// their exact JSON digest encoding and all pre-migration receipts.
type DeliveryConfigurationInput struct {
	Type                  string
	Classification        string
	ExpectedAuthorization *ConfigurationAuthorization
	Confirmed             bool
}

type ConfigurationAuthorization struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func IsDeliveryConfiguration(kind string) bool {
	return kind == "component.create" || kind == "environment.create" || kind == "environment.authorization.grant" || kind == "environment.authorization.revoke"
}

func ValidComponentType(value string) bool {
	switch value {
	case "service", "web", "client", "library", "data-pipeline", "infrastructure", "other":
		return true
	}
	return false
}

func validateDeliveryConfiguration(input ConfigurationInput, workspace string) (string, bool) {
	d := input.Delivery
	if d == nil || input.Visibility != "" || input.InitialAdminUserID != "" || input.MemberUserIDs != nil || input.Role != "" || input.ExpectedRole != nil || input.ExpectedMember {
		return "", false
	}
	if input.Kind == "component.create" || input.Kind == "environment.create" {
		if input.ScopeID != workspace || input.UserID != "" || !projectKey.MatchString(input.Key) || !ValidConfigurationID(input.OwnerTeamID, "tem_") || d.ExpectedAuthorization != nil || d.Confirmed {
			return "", false
		}
		if input.Kind == "component.create" {
			return "cmp_", ValidComponentType(d.Type) && d.Classification == ""
		}
		return "env_", d.Classification == "staging" && d.Type == ""
	}
	if !ValidConfigurationID(input.ScopeID, "env_") || !ValidConfigurationID(input.UserID, "usr_") || !d.Confirmed || d.Type != "" || d.Classification != "" || input.Name != "" || input.Key != "" || input.OwnerTeamID != "" {
		return "", false
	}
	if a := d.ExpectedAuthorization; a != nil && (!ValidConfigurationID(a.ID, "dpa_") || (a.Status != "active" && a.Status != "revoked")) {
		return "", false
	}
	return "", true
}
