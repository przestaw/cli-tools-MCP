package output

import "workctl/internal/azure/client"

// UserName renders an identity reference as a display name, falling back to the
// unique name (email) when no display name is present.
func UserName(u client.IdentityRef) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.UniqueName != "" {
		return u.UniqueName
	}
	return u.ID
}
