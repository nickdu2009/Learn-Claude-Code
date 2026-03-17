# Auth Migration Runbook: Legacy Tokens → Signed Session Tokens

## Overview
This runbook documents the migration process from legacy authentication tokens to cryptographically signed session tokens.

## Objectives
- Replace insecure legacy token format with JWT-based signed session tokens
- Maintain backward compatibility during transition period
- Ensure zero downtime during migration
- Preserve all existing user session data and permissions

## Pre-Migration Preparation
1. Verify all services are running on compatible versions
2. Backup current token signing keys and configuration
3. Deploy new token validation logic to all services
4. Configure dual-token support mode (legacy + signed)
5. Test migration scripts in staging environment

## Migration Procedure
1. Enable dual-token mode across all services
2. Begin issuing signed session tokens alongside legacy tokens
3. Gradually shift traffic to prefer signed tokens
4. Monitor metrics for token validation errors and performance
5. Once stable, disable legacy token issuance
6. Complete migration by disabling legacy token validation

## Rollback Procedure
1. Re-enable legacy token issuance in configuration
2. Deploy rollback configuration to all services
3. Verify legacy token validation is functioning
4. Monitor for any authentication failures
5. If issues persist beyond 15 minutes, escalate to incident response

## Post-Migration Validation
1. Confirm all active sessions use signed tokens
2. Verify token expiration and refresh behavior
3. Test edge cases: token revocation, permission changes, session timeouts
4. Validate audit logs show proper token usage patterns
5. Confirm monitoring dashboards reflect expected metrics

## Troubleshooting
- Token validation failures: Check clock skew, key rotation status, signature algorithm
- Session inconsistencies: Verify session store synchronization
- Performance degradation: Monitor JWT parsing overhead and cache hit rates
- Permission mismatches: Validate claims mapping between legacy and signed formats