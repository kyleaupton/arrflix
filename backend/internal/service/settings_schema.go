package service

type SettingType string

const (
	SettingText SettingType = "text"
	SettingBool SettingType = "bool"
	SettingInt  SettingType = "int"
	SettingJSON SettingType = "json"
)

type SettingSpec struct {
	Key       string
	Type      SettingType
	Default   any
	Sensitive bool
}

// Registry enumerates all supported settings and their types/defaults.
// Extend this map as your application grows.
var Registry = map[string]SettingSpec{
	"site.title":            {Key: "site.title", Type: SettingText, Default: "Arrflix"},
	"auth.signup_strategy":  {Key: "auth.signup_strategy", Type: SettingText, Default: "invite_only"},
	"requests.max_per_user": {Key: "requests.max_per_user", Type: SettingInt, Default: int64(5)},
	"tmdb.api_key":          {Key: "tmdb.api_key", Type: SettingText, Default: "", Sensitive: true},
}
