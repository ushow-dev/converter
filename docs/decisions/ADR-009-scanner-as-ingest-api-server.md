# ADR-009: Scanner as Ingest API Server

**Status:** accepted
**Date:** 2026-03-18

## Context

Originally the scanner called the converter API to register files and poll status
(`POST /api/ingest/incoming/register`, `GET /api/ingest/incoming/{id}`).
The IngestWorker (Go) called the converter API for claim/progress/complete/fail.

This created bidirectional coupling: scanner knew about converter, and converter
owned ingest state.

## Decision

Invert the relationship. The **Scanner** is now the HTTP server — it owns all ingest
state and exposes `/api/v1/incoming/{claim,<id>/progress,<id>/complete,<id>/fail}`.
The **IngestWorker** (Go) polls the scanner instead of the converter.

The IngestWorker creates the `media_job` and pushes to `convert_queue` locally
(previously done by the converter's Complete endpoint).

The converter API no longer has any ingest-related code.

## Consequences

- Scanner has zero knowledge of converter — simpler, more deployable independently
- IngestWorker needs `SCANNER_API_URL` instead of `CONVERTER_API_URL`
- `incoming_media_items` table dropped from converter DB (migration 013)
- Scanner gets `claimed_at`/`claim_expires_at` columns (migration 002) for TTL claims
- The `INGEST_SERVICE_TOKEN` is now verified by the scanner (not the converter)
