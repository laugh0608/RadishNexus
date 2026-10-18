// Run with the fixed image's bundled Groovy, while collector is stopped.
// Cleanup is exact-build and opt-in. No command sends a network request.
import groovy.json.JsonOutput
import java.nio.file.Paths

boolean basic = args.length == 2 && args[0] in ['init', 'status']
boolean plan = args.length == 3 && args[0] == 'cleanup-plan' && args[2] ==~ /[1-9][0-9]{0,9}/
boolean cleanup = args.length == 4 && args[0] == 'cleanup' && args[2] ==~ /[1-9][0-9]{0,9}/ && args[3] == '--confirmed'
if (!(basic || plan || cleanup)) {
    System.err.println('usage: offline.groovy init|status CONFIG; cleanup-plan CONFIG BUILD; cleanup CONFIG BUILD --confirmed')
    System.exit(2)
}
try {
    if (args[0] == 'status' || plan) {
        // Read-only status works without taking the live collector lock.
        def config = Collector.loadConfig(Paths.get(args[1]))
        def state = Collector.checkpoint(config)
        if (plan) {
            println JsonOutput.toJson(Collector.cleanupPlan(config, state, args[2].toLong(), java.time.Instant.now()))
            return
        }
        long age = java.time.Duration.between(java.time.Instant.parse(state.observed_at), java.time.Instant.now()).seconds
        println JsonOutput.toJson([source_id: config.binding.source_id, observed_at: state.observed_at,
            last_sweep_at: state.last_sweep_at, lifecycle: state.lifecycle, error: state.error,
            continuous_prefix: state.prefix, captured_high: state.high, scan_high: state.scan_high, next_scan: state.cursor,
            entries: state.entries.collectEntries { String n, Map e -> [(n): [kind: e.kind, acknowledged: e.handoff_at != null]] },
            heartbeat_age_seconds: age, stale: age > 180 || age < -5, cleanup_enabled: false, cleanup_mode: 'explicit_build_only'])
        if (state.lifecycle != 'running' || age > 180 || age < -5) System.exit(1)
    } else if (cleanup) {
        def core = new Collector(Paths.get(args[1]))
        def cleaned
        try {
            cleaned = core.cleanup(args[2].toLong())
        } finally { core.close() }
        println JsonOutput.toJson([source_id: cleaned.source_id, build_number: cleaned.build_number,
            state: 'retired', already_retired: cleaned.already_retired])
    } else {
        new Collector(Paths.get(args[1]), true).close()
        println 'collector_initialized'
    }
} catch (Throwable error) {
    System.err.println(Collector.safeCode(error)); System.exit(1)
}
