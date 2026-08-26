package enrichment

type AdmissionResult uint8

const (
	AdmissionInvalid AdmissionResult = iota
	AdmissionCached
	AdmissionCoalesced
	AdmissionEnqueued
	AdmissionDropped
)

func (result AdmissionResult) Accepted() bool {
	return result == AdmissionCached || result == AdmissionCoalesced || result == AdmissionEnqueued
}
