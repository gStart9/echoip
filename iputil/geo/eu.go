package geo

// euMembers holds the ISO 3166-1 alpha-2 codes of the European Union's member states.
var euMembers = map[string]bool{
	"AT": true, "BE": true, "BG": true, "CY": true, "CZ": true, "DE": true, "DK": true,
	"EE": true, "ES": true, "FI": true, "FR": true, "GR": true, "HR": true, "HU": true,
	"IE": true, "IT": true, "LT": true, "LU": true, "LV": true, "MT": true, "NL": true,
	"PL": true, "PT": true, "RO": true, "SE": true, "SI": true, "SK": true,
}

func isEU(iso string) bool { return euMembers[iso] }
