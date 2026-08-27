package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

func (store *Store) RecordRouteSightings(ctx context.Context, batch storage.RouteSightingsBatch) error {
	if batch.GuildID == 0 || len(batch.Observations) == 0 {
		return nil
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin route sightings batch: %w", err)
	}
	if err := store.recordRouteSightingsTx(ctx, transaction, batch); err != nil {
		_ = transaction.Rollback()
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit route sightings batch: %w", err)
	}
	return nil
}

func (store *Store) recordRouteSightingsTx(ctx context.Context, transaction *sql.Tx, batch storage.RouteSightingsBatch) error {
	feederID := batch.FeederID
	if feederID == "" {
		feederID = domain.FeederAll
	}
	bucket := batch.BucketStart.UTC().Truncate(time.Hour)
	if bucket.IsZero() {
		bucket = time.Now().UTC().Truncate(time.Hour)
	}
	catalogStatement := `INSERT INTO route_catalog(
		source, callsign, airline_name, airline_icao, airline_iata,
		origin_icao, origin_iata, origin_name, origin_country_iso,
		destination_icao, destination_iata, destination_name, destination_country_iso,
		plausible, plausibility_known, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(callsign) DO UPDATE SET
		source=excluded.source,
		airline_name=excluded.airline_name,
		airline_icao=excluded.airline_icao,
		airline_iata=excluded.airline_iata,
		origin_icao=excluded.origin_icao,
		origin_iata=excluded.origin_iata,
		origin_name=excluded.origin_name,
		origin_country_iso=excluded.origin_country_iso,
		destination_icao=excluded.destination_icao,
		destination_iata=excluded.destination_iata,
		destination_name=excluded.destination_name,
		destination_country_iso=excluded.destination_country_iso,
		plausible=excluded.plausible,
		plausibility_known=excluded.plausibility_known,
		updated_at=excluded.updated_at`
	sightingStatement := `INSERT INTO route_sightings(guild_id, feeder_id, icao, callsign, bucket_start, sightings) VALUES (?, ?, ?, ?, ?, 1)
ON CONFLICT(guild_id, feeder_id, icao, bucket_start) DO UPDATE SET
	callsign=excluded.callsign,
	sightings=sightings+1`
	for _, observation := range batch.Observations {
		route := observation.Route
		if route.Source != domain.DataSourceADSBLOL {
			return fmt.Errorf("persist route source %q: only adsb.lol is allowed", route.Source)
		}
		_, err := transaction.ExecContext(ctx, catalogStatement,
			string(route.Source), route.Callsign, route.AirlineName, route.AirlineICAO, route.AirlineIATA,
			route.OriginICAO, route.OriginIATA, route.OriginName, route.OriginCountryISO,
			route.DestinationICAO, route.DestinationIATA, route.DestinationName, route.DestinationCountryISO,
			boolInt(route.Plausible), boolInt(route.PlausibilityKnown), formatTime(route.UpdatedAt.UTC()),
		)
		if err != nil {
			return fmt.Errorf("upsert route catalog: %w", err)
		}
		_, err = transaction.ExecContext(ctx, sightingStatement, batch.GuildID, feederID, observation.ICAO, observation.Callsign, formatTime(bucket))
		if err != nil {
			return fmt.Errorf("upsert route sighting: %w", err)
		}
	}
	return nil
}

func (store *Store) TopRouteRankings(ctx context.Context, guildID uint64, metric, period string, limit int, domesticCountryISO string) ([]storage.RouteRankingRow, error) {
	return store.TopRouteRankingsForScope(ctx, guildID, domain.FeederAll, metric, period, limit, domesticCountryISO)
}

func (store *Store) TopRouteRankingsForScope(ctx context.Context, guildID uint64, feederScope domain.FeederID, metric, period string, limit int, domesticCountryISO string) ([]storage.RouteRankingRow, error) {
	if guildID == 0 {
		return nil, fmt.Errorf("guild id is required")
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 25 {
		limit = 25
	}
	since, ok := routeRankingSince(period)
	if !ok {
		return nil, fmt.Errorf("unsupported route ranking period %q", period)
	}
	domesticCountryISO = strings.ToUpper(strings.TrimSpace(domesticCountryISO))
	if feederScope == "" {
		feederScope = domain.FeederAll
	}
	baseWhere := `rs.guild_id = ? AND rs.feeder_id = ? AND rs.bucket_start >= ? AND rc.origin_iata != '' AND rc.destination_iata != ''
		AND rc.origin_iata != rc.destination_iata
		AND (rc.plausibility_known = 0 OR rc.plausible = 1)`
	baseArgs := []any{guildID, feederScope, formatTime(since)}
	args := append([]any(nil), baseArgs...)
	var query string
	switch metric {
	case "routes":
		query = fmt.Sprintf(`
			SELECT rc.origin_iata || ' → ' || rc.destination_iata AS label,
				trim(rc.origin_name || ' → ' || rc.destination_name) AS detail,
				SUM(rs.sightings) AS count
			FROM route_sightings rs
			INNER JOIN route_catalog rc ON rs.callsign = rc.callsign
			WHERE %s
			GROUP BY rc.origin_iata, rc.destination_iata, rc.origin_name, rc.destination_name
			ORDER BY count DESC, label ASC
			LIMIT ?`, baseWhere)
	case "origin-countries":
		query = fmt.Sprintf(`
			SELECT rc.origin_country_iso AS label, '' AS detail, SUM(rs.sightings) AS count
			FROM route_sightings rs
			INNER JOIN route_catalog rc ON rs.callsign = rc.callsign
			WHERE %s AND rc.origin_country_iso != '' AND rc.origin_country_iso != rc.destination_country_iso
			GROUP BY rc.origin_country_iso
			ORDER BY count DESC, label ASC
			LIMIT ?`, baseWhere)
	case "destination-countries":
		query = fmt.Sprintf(`
			SELECT rc.destination_country_iso AS label, '' AS detail, SUM(rs.sightings) AS count
			FROM route_sightings rs
			INNER JOIN route_catalog rc ON rs.callsign = rc.callsign
			WHERE %s AND rc.destination_country_iso != '' AND rc.origin_country_iso != rc.destination_country_iso
			GROUP BY rc.destination_country_iso
			ORDER BY count DESC, label ASC
			LIMIT ?`, baseWhere)
	case "airlines":
		query = fmt.Sprintf(`
			SELECT COALESCE(NULLIF(rc.airline_name, ''), NULLIF(rc.airline_icao, ''), rc.callsign) AS label,
				trim(rc.airline_icao || CASE WHEN rc.airline_iata != '' THEN ' / ' || rc.airline_iata ELSE '' END) AS detail,
				SUM(rs.sightings) AS count
			FROM route_sightings rs
			INNER JOIN route_catalog rc ON rs.callsign = rc.callsign
			WHERE %s AND (rc.airline_name != '' OR rc.airline_icao != '')
			GROUP BY label, detail
			ORDER BY count DESC, label ASC
			LIMIT ?`, baseWhere)
	case "domestic-airports":
		if domesticCountryISO == "" {
			return nil, fmt.Errorf("domestic country is not configured")
		}
		query = fmt.Sprintf(`
			SELECT airport_code AS label, airport_name AS detail, SUM(sightings) AS count FROM (
				SELECT rc.origin_iata AS airport_code, rc.origin_name AS airport_name, rs.sightings
				FROM route_sightings rs
				INNER JOIN route_catalog rc ON rs.callsign = rc.callsign
				WHERE %s AND rc.origin_country_iso = ? AND rc.origin_iata != ''
				UNION ALL
				SELECT rc.destination_iata AS airport_code, rc.destination_name AS airport_name, rs.sightings
				FROM route_sightings rs
				INNER JOIN route_catalog rc ON rs.callsign = rc.callsign
				WHERE %s AND rc.destination_country_iso = ? AND rc.destination_iata != ''
			) airports
			GROUP BY airport_code, airport_name
			ORDER BY count DESC, label ASC
			LIMIT ?`, baseWhere, baseWhere)
		args = append(args, domesticCountryISO)
		args = append(args, baseArgs...)
		args = append(args, domesticCountryISO)
	case "international-airports":
		if domesticCountryISO == "" {
			return nil, fmt.Errorf("domestic country is not configured")
		}
		query = fmt.Sprintf(`
			SELECT airport_code AS label, airport_name AS detail, SUM(sightings) AS count FROM (
				SELECT rc.origin_iata AS airport_code, rc.origin_name AS airport_name, rs.sightings
				FROM route_sightings rs
				INNER JOIN route_catalog rc ON rs.callsign = rc.callsign
				WHERE %s AND rc.origin_country_iso != ? AND rc.origin_iata != ''
				UNION ALL
				SELECT rc.destination_iata AS airport_code, rc.destination_name AS airport_name, rs.sightings
				FROM route_sightings rs
				INNER JOIN route_catalog rc ON rs.callsign = rc.callsign
				WHERE %s AND rc.destination_country_iso != ? AND rc.destination_iata != ''
			) airports
			GROUP BY airport_code, airport_name
			ORDER BY count DESC, label ASC
			LIMIT ?`, baseWhere, baseWhere)
		args = append(args, domesticCountryISO)
		args = append(args, baseArgs...)
		args = append(args, domesticCountryISO)
	default:
		return nil, fmt.Errorf("unsupported route ranking metric %q", metric)
	}
	args = append(args, limit)
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query route rankings: %w", err)
	}
	defer rows.Close()
	result := make([]storage.RouteRankingRow, 0, limit)
	for rows.Next() {
		var row storage.RouteRankingRow
		if err := rows.Scan(&row.Label, &row.Detail, &row.Count); err != nil {
			return nil, fmt.Errorf("scan route ranking: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (store *Store) RouteTrafficCounts(ctx context.Context, guildID uint64, since time.Time) (storage.RouteTrafficCounts, error) {
	var counts storage.RouteTrafficCounts
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM route_catalog`).Scan(&counts.CatalogEntries); err != nil {
		return storage.RouteTrafficCounts{}, fmt.Errorf("count route catalog: %w", err)
	}
	since = since.UTC()
	if since.IsZero() {
		since = time.Unix(0, 0).UTC()
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(sightings), 0) FROM route_sightings WHERE guild_id=? AND feeder_id='all' AND bucket_start>=?`, guildID, formatTime(since)).Scan(&counts.Sightings); err != nil {
		return storage.RouteTrafficCounts{}, fmt.Errorf("count route sightings: %w", err)
	}
	return counts, nil
}

func routeRankingSince(period string) (time.Time, bool) {
	now := time.Now().UTC()
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "24h", "day":
		return now.Add(-24 * time.Hour), true
	case "7d", "week":
		return now.Add(-7 * 24 * time.Hour), true
	case "30d", "month":
		return now.Add(-30 * 24 * time.Hour), true
	case "all", "all-time":
		return time.Unix(0, 0).UTC(), true
	default:
		return time.Time{}, false
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
