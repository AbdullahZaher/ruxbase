---
title: "Field types and record values"
description: "Map collection fields to the API values and PostgreSQL-specific storage behavior."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Choose fields
The record API returns logical field values, not a raw database row. Application fields are defined by each collection; check required, hidden and other constraints before generating client types.

| Field type | Client value or purpose |
| --- | --- |
| text, email, url, editor | String |
| number, bool | Number, boolean |
| date, autodate | API date string; autodate is managed by configuration |
| select | String or array according to cardinality |
| relation | Record ID or array of IDs |
| file | Stored filename or array; upload separately as multipart |
| json | JSON value |
| geoPoint | Geographic point object |
| password | Write-only sensitive field; never expect password in records |
| search | Generated search field; no record value in API |
| vector | Numeric array, or null when empty |

Ruxbase stores dates as TIMESTAMPTZ, multiple values and JSON as JSONB, and numbers as DOUBLE PRECISION. An empty date still returns an empty string through the API. JSONB does not preserve input formatting or object key order. Do not compare raw serialized JSON for identity.

## Modeling workflow
Start from [a posts schema](/ruxbase/integrations/example-app/). Create test records with required fields, validate omitted and invalid values, and test the actual response including hidden fields. Use explicit sort when ordering matters. See [search fields](/ruxbase/data/full-text-search/) and [vector fields](/ruxbase/data/vector-search/) before choosing PostgreSQL-native types.

## Evidence
[Source: core/field_text.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/field_text.go), [Source: core/field_relation.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/field_relation.go), [Source: core/field_file.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/field_file.go), [Source: core/field_search.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/field_search.go), [Source: core/field_vector.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/field_vector.go); [documented type mapping](/ruxbase/migration/compatibility/). Field-specific options and validation are governed by source, not a universal schema.
