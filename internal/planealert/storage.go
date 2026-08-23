package planealert

import (
	"time"

	"github.com/j4v3l/SkyFeed/internal/storage"
)

func StorageReference(record Record, commitHash string) storage.PlaneAlertReference {
	return storage.PlaneAlertReference{
		ICAO:         record.ICAO,
		Registration: record.Registration,
		Operator:     record.Operator,
		AircraftType: record.Type,
		ICAOType:     record.ICAOType,
		FlightGroup:  record.Group,
		Tag1:         record.Tag1,
		Tag2:         record.Tag2,
		Tag3:         record.Tag3,
		Category:     record.Category,
		Link:         record.Link,
		ImageLink1:   record.Image1,
		ImageLink2:   record.Image2,
		ImageLink3:   record.Image3,
		ImageLink4:   record.Image4,
		CommitHash:   commitHash,
		UpdatedAt:    time.Now().UTC(),
	}
}

func RecordFromStorage(reference storage.PlaneAlertReference) Record {
	return Record{
		ICAO:         reference.ICAO,
		Registration: reference.Registration,
		Operator:     reference.Operator,
		Type:         reference.AircraftType,
		ICAOType:     reference.ICAOType,
		Group:        reference.FlightGroup,
		Tag1:         reference.Tag1,
		Tag2:         reference.Tag2,
		Tag3:         reference.Tag3,
		Category:     reference.Category,
		Link:         reference.Link,
		Image1:       reference.ImageLink1,
		Image2:       reference.ImageLink2,
		Image3:       reference.ImageLink3,
		Image4:       reference.ImageLink4,
	}
}
