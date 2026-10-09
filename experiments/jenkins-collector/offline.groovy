// Run with the fixed image's bundled Groovy, while collector is stopped.
// Commands intentionally cannot send network requests or delete snapshots.
import groovy.json.JsonOutput
import java.nio.file.Paths

if (args.length != 2 || !(args[0] in ['init', 'status'])) {
    System.err.println('usage: offline.groovy init|status /absolute/collector-config.json')
    System.exit(2)
}
try {
    if (args[0] == 'status') {
        // Read-only status works without taking the live collector lock.
        def config = Collector.loadConfig(Paths.get(args[1]))
        def state = StrictJson.parse(Collector.read(Paths.get(config.state_dir, 'checkpoint.json'), Collector.STATE_LIMIT))
        Collector.require(StrictJson.parse(Collector.read(Paths.get(config.state_dir, 'manifest.json'), 16384)) == config.binding,
            'collector_binding_mismatch')
        Collector.validateState(state, config)
        long age = java.time.Duration.between(java.time.Instant.parse(state.observed_at), java.time.Instant.now()).seconds
        println JsonOutput.toJson([source_id: config.binding.source_id, observed_at: state.observed_at,
            last_sweep_at: state.last_sweep_at, lifecycle: state.lifecycle, error: state.error,
            continuous_prefix: state.prefix, captured_high: state.high, scan_high: state.scan_high, next_scan: state.cursor,
            entries: state.entries.collectEntries { String n, Map e -> [(n): [kind: e.kind, acknowledged: e.handoff_at != null]] },
            heartbeat_age_seconds: age, stale: age > 180 || age < -5, cleanup_enabled: false])
        if (state.lifecycle != 'running' || age > 180 || age < -5) System.exit(1)
    } else {
        new Collector(Paths.get(args[1]), true).close()
        println 'collector_initialized'
    }
} catch (Throwable error) {
    System.err.println(Collector.safeCode(error)); System.exit(1)
}
