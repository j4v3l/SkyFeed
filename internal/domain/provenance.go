package domain

// DataSource identifies a presentation or enrichment provider. It is separate
// from ProviderID because enrichment is never authoritative live aircraft data.
type DataSource string

const (
	DataSourceUnknown         DataSource = "unknown"
	DataSourceADSBLOL         DataSource = "adsb-lol"
	DataSourceADSBDB          DataSource = "adsbdb"
	DataSourceAviationWeather DataSource = "aviationweather.gov"
	DataSourcePlaneAlert      DataSource = "plane-alert-db"
)

func (source DataSource) Known() bool { return source != "" && source != DataSourceUnknown }
