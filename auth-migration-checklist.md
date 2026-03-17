# Auth Migration Checklist

## Pre-Migration Verification
- [ ] All services deployed with dual-token support capability
- [ ] Token signing keys securely stored and accessible
- [ ] Staging environment migration successfully completed
- [ ] Monitoring dashboards configured for token validation metrics
- [ ] Rollback plan documented and approved

## During-Migration Verification
- [ ] Dual-token mode enabled across all services
- [ ] Signed tokens being issued alongside legacy tokens
- [ ] Token validation error rate < 0.1%
- [ ] Session establishment latency within acceptable thresholds
- [ ] No authentication failures reported in logs

## Post-Migration Verification
- [ ] Legacy token issuance disabled
- [ ] All active sessions using signed tokens only
- [ ] Token refresh functionality working correctly
- [ ] Permission claims properly mapped from legacy format
- [ ] Audit logs showing complete migration completion

## Critical Success Criteria
- [ ] Zero authentication downtime during migration window
- [ ] All user sessions maintained without interruption
- [ ] No security vulnerabilities introduced in token handling
- [ ] Performance impact within acceptable thresholds (<5% latency increase)
- [ ] Full rollback capability verified and documented