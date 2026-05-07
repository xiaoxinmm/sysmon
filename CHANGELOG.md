# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Fixed - Traffic Rate Calculation (2026-05-07)

#### Critical Bug Fixes

**1. Incorrect Instantaneous Rate Calculation**
- **Problem**: In `Update()` methods, the rate was calculated by dividing a single packet's bytes by the time since last update, causing massive overestimation
- **Example**: A 1500-byte packet arriving 0.001s after the previous one would show 1.5 MB/s instead of actual rate
- **Fix**: Added minimum threshold (0.001s) to avoid division by very small values, and clarified this is for peak detection only

**2. Wrong Average Rate Calculation in GetSnapshot()**
- **Problem**: Used `LastUpdate` time instead of sampling period start time, resulting in incorrect average rates
- **Fix**: Added `StartTime` field to `TrafficStats` to track the entire sampling period
- **Impact**: Average rates now accurately reflect bytes/second over the full collection window

**3. Inconsistent Rate Calculation in GetRealTimeStats()**
- **Problem**: Used different fallback values (60s vs 1s) and wrong time reference
- **Fix**: Unified to use `StartTime` with consistent 0.1s minimum threshold

**4. Small Anomaly Detection Window**
- **Problem**: Window size of 10 samples (10 minutes with 60s interval) was too small for reliable anomaly detection
- **Fix**: Increased to 30 samples (30 minutes) for better baseline establishment
- **Impact**: Reduces false positives in anomaly detection

#### Technical Details

**Before:**
```go
// Wrong: dividing single packet size by inter-packet time
elapsed := now.Sub(stats.LastUpdate).Seconds()
currentRate := float64(bytes) / elapsed  // Massive overestimation
```

**After:**
```go
// Correct: track sampling period and use for average calculation
stats.StartTime = now  // Set when stats created
elapsed := now.Sub(stats.StartTime).Seconds()
avgRate := float64(stats.Bytes) / elapsed  // Accurate average
```

#### Rate Calculation Semantics

- **Instantaneous Rate** (for peak detection): Current packet size / time since last packet
  - Used only for `PeakRate` tracking
  - Has minimum 0.001s threshold to avoid extreme values
  
- **Average Rate** (for reporting): Total bytes / sampling period duration
  - Used in snapshots and real-time stats
  - Reflects actual throughput over the collection window
  - Has minimum 0.1s threshold for very short-lived connections

#### Files Modified

- `traffic/types.go`: Added `StartTime` field to `TrafficStats`
- `traffic/aggregator.go`: Fixed all rate calculation methods
- `main.go`: Increased anomaly detection window from 10 to 30 samples

#### Testing

Compiled and tested successfully:
- Binary size: 24MB
- Startup: Normal
- Traffic monitoring: Active on eth0
- No compilation errors or warnings

### Notes

All rate values are in **bytes per second (B/s)**. Frontend should convert to KB/s or MB/s for display.

## [v2.0.0] - 2026-05-05

### Added
- Unified system and traffic monitoring
- SQLite-based data persistence
- Network traffic capture and analysis
- Anomaly detection for traffic patterns
- Agent mode for distributed monitoring
- Web-based terminal (optional)
- Prometheus metrics export

### Changed
- Migrated from JSON file storage to SQLite
- Improved WebSocket real-time updates
- Enhanced security with separate shell authentication

### Fixed
- Memory leaks in packet capture
- Race conditions in aggregator
- Docker container detection on various platforms
