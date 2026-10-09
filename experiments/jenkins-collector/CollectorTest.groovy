import java.nio.file.*
import java.nio.file.attribute.PosixFilePermissions
import java.time.Instant

def privateFile = PosixFilePermissions.asFileAttribute(Collector.PRIVATE_FILE)
def fixture = { Closure clock = { Instant.now() } ->
    Path root = Files.createTempDirectory('collector-test-', PosixFilePermissions.asFileAttribute(Collector.PRIVATE_DIR))
    ['input', 'state', 'ack'].each { Files.createDirectory(root.resolve(it), PosixFilePermissions.asFileAttribute(Collector.PRIVATE_DIR)) }
    Map config = [version: 1, input_dir: root.resolve('input').toString(), state_dir: root.resolve('state').toString(),
        ack_dir: root.resolve('ack').toString(), binding: [version: 1, receiver_id: 'receiver_test', jenkins_id: 'jenkins_test',
        origin: 'https://receiver.test', source_id: 'source_test', workspace_id: 'wrk_test', component_id: 'cmp_test',
        job_full_name: 'collector-probe', first_build_number: 1]]
    Path file = Files.createFile(root.resolve('config.json'), privateFile)
    Files.write(file, Collector.encode(config))
    new Collector(file, true, null, clock).close()
    [root: root, file: file, config: config]
}
def payload = { long n, String result = 'SUCCESS' ->
    [version: 1, job_full_name: 'collector-probe', build_number: n, building: false, in_progress: false,
        result: result, started_at: '2026-01-01T00:00:00Z', completed_at: '2026-01-01T00:00:01Z']
}
def fails = { String code, Closure operation ->
    try { operation(); assert false: "expected ${code}" }
    catch (CollectorFailure e) { assert e.message == code }
}
def parseFails = { String input ->
    boolean rejected = false
    try { StrictJson.parse(input.getBytes('UTF-8')) } catch (Exception e) { rejected = true }
    assert rejected
}
['{"x":1,"x":2}', '{"x":1,}', '{"x":1} false', '{"x":', '{"x":[1]}', '{"x":NaN}'].each(parseFails)
assert StrictJson.parse('{"x":true,"y":null,"nested":{"v":"中文"}}'.getBytes('UTF-8')).nested.v == '中文'

// Missing callback and reverse completion order. Reopen keeps the low gap.
def f = fixture()
def c = new Collector(f.file)
c.finalized(2, 2, { payload(it) })
assert c.status().continuous_prefix == 0
c.reconcile(2, { it == 1 ? [pending: true] : payload(it) })
assert c.status().counts.running == 1
c.close(); c = new Collector(f.file)
c.reconcile(2, { payload(it) })
assert c.status().continuous_prefix == 2
assert c.status().counts.published == 2
byte[] original = Collector.read(f.root.resolve('input/build-2.json'), Collector.PAYLOAD_LIMIT)
// Existing finalized snapshot is immutable and does not depend on retained Run.
c.reconcile(2, { throw new IOException('historical source unavailable') })
assert Arrays.equals(original, Collector.read(f.root.resolve('input/build-2.json'), Collector.PAYLOAD_LIMIT))
fails('collector_locked') { new Collector(f.file) }
c.close()

// A second callback with changed terminal bytes must not silently reuse input.
f = fixture(); c = new Collector(f.file)
c.finalized(1, 1, { payload(it) })
fails('collector_input_conflict') { c.finalized(1, 1, { payload(it, 'FAILURE') }) }
c.close()

// Batch boundary, retained missing numbers, and repeated passes.
f = fixture(); c = new Collector(f.file)
c.reconcile(205, { it == 3 ? null : payload(it) })
assert c.status().counts.published == 99 && c.status().next_scan == 101 && c.status().unscanned == 105
c.close(); c = new Collector(f.file)
c.reconcile(305, { payload(it) }); assert c.status().next_scan == 201 && c.status().scan_high == 205
c.reconcile(405, { payload(it) }); assert c.status().next_scan == 1 && c.status().continuous_prefix == 2
c.reconcile(505, { payload(it) }); assert c.status().continuous_prefix == 205 && c.status().scan_high == 505
c.close()

// Deleted history is reported without falsely claiming complete coverage.
f = fixture(); c = new Collector(f.file)
c.reconcile(3, { it == 1 ? null : (it == 2 ? { throw new IOException() }() : payload(it)) })
assert c.status().counts.missing == 1 && c.status().counts.unreadable == 1 && c.status().continuous_prefix == 0
c.close()

// Unsupported terminal result remains honest input for worker blocking.
f = fixture(); c = new Collector(f.file)
c.reconcile(5, { payload(it, ['SUCCESS','FAILURE','ABORTED','UNSTABLE','NOT_BUILT'][(int) it - 1]) })
assert StrictJson.parse(Collector.read(f.root.resolve('input/build-4.json'), 16384)).result == 'UNSTABLE'
// Acknowledgment requires exact source, number and bytes digest.
Path a = Files.createFile(f.root.resolve('ack/build-1.json'), privateFile)
Files.write(a, Collector.encode([version: 1, source_id: 'source_test', build_number: 1,
    payload_sha256: Collector.digest(Collector.read(f.root.resolve('input/build-1.json'), 16384))]))
c.reconcile(5, { payload(it) }); assert c.status().counts.acknowledged == 1
c.close(); c = new Collector(f.file); assert c.status().counts.acknowledged == 1
Files.write(a, Collector.encode([version: 1, source_id: 'other_source', build_number: 1, payload_sha256: '0' * 64]))
fails('collector_ack_conflict') { c.reconcile(5, { payload(it) }) }
assert c.status().lifecycle == 'paused'; c.close()

// Immutable input corruption and known job-number rewind halt the source.
f = fixture(); c = new Collector(f.file); c.reconcile(2, { payload(it) })
Files.write(f.root.resolve('input/build-1.json'), Collector.encode(payload(1, 'FAILURE')))
fails('collector_input_conflict') { c.reconcile(2, { payload(it) }) }
fails('collector_paused') { c.reconcile(2, { payload(it) }) }; c.close()
f = fixture(); c = new Collector(f.file); c.reconcile(2, { payload(it) })
fails('collector_source_rewind') { c.reconcile(1, { payload(it) }) }; c.close()

// Recover an externally lost published file only from matching retained history.
f = fixture(); c = new Collector(f.file); c.reconcile(1, { payload(it) })
Files.delete(f.root.resolve('input/build-1.json')); c.reconcile(1, { payload(it) })
assert Files.exists(f.root.resolve('input/build-1.json'))
Files.delete(f.root.resolve('input/build-1.json'))
fails('collector_published_input_missing') { c.reconcile(1, { null }) }; c.close()

// Atomic failures before publication and after rename never advance the prefix.
['write', 'file_sync', 'rename', 'directory_sync'].each { stage ->
    f = fixture()
    boolean inject = true
    c = new Collector(f.file, false, { point -> if (inject && point == stage) throw new IOException('synthetic') })
    try { c.reconcile(1, { payload(it) }); assert false } catch (IOException expected) { }
    assert c.status().continuous_prefix == 0 && c.status().lifecycle == 'paused'
    c.close(); c = new Collector(f.file)
    c.reconcile(1, { payload(it) }); assert c.status().continuous_prefix == 1; c.close()
}
// Snapshot succeeds, checkpoint commit fails: adopt only matching history bytes.
f = fixture(); int writes = 0
c = new Collector(f.file, false, { stage -> if (stage == 'write' && ++writes >= 2) throw new IOException() })
try { c.reconcile(1, { payload(it) }); assert false } catch (IOException expected) { }
c.close(); c = new Collector(f.file)
c.reconcile(1, { payload(it) }); assert c.status().continuous_prefix == 1; c.close()

// Source binding is not mutable, and unsafe filesystem objects are refused.
f = fixture(); Map changed = f.config; changed.binding.receiver_id = 'different_receiver'
Files.write(f.file, Collector.encode(changed))
fails('collector_binding_mismatch') { new Collector(f.file) }
f = fixture(); Path link = f.root.resolve('input/build-1.json')
Files.createSymbolicLink(link, f.file)
fails('collector_unsafe_file') { new Collector(f.file) }
f = fixture(); Files.createLink(f.root.resolve('input/build-1.json'), f.file)
fails('collector_unsafe_file') { new Collector(f.file) }
f = fixture(); Files.setPosixFilePermissions(f.root.resolve('input'), PosixFilePermissions.fromString('rwxr-xr-x'))
fails('collector_unsafe_directory') { new Collector(f.file) }
f = fixture(); c = new Collector(f.file)
fails('collector_capacity') { c.reconcile(10001, { payload(it) }) }; c.close()
f = fixture(); Files.write(f.root.resolve('state/checkpoint.json'), '{"version":999}'.getBytes('UTF-8'))
fails('collector_invalid_state') { new Collector(f.file) }

// Process termination releases the OS lock, while durable snapshots survive.
f = fixture()
Path crash = Files.createFile(f.root.resolve('crash.groovy'), privateFile)
Files.write(crash, '''
def c = new Collector(java.nio.file.Paths.get(args[0]))
c.reconcile(1, { long n -> [version:1, job_full_name:'collector-probe', build_number:n, building:false, in_progress:false,
    result:'SUCCESS', started_at:'2026-01-01T00:00:00Z', completed_at:'2026-01-01T00:00:01Z'] })
Runtime.runtime.halt(23)
'''.getBytes('UTF-8'))
def process = new ProcessBuilder('java', '-cp', System.getProperty('java.class.path'), 'groovy.ui.GroovyMain', crash.toString(), f.file.toString()).inheritIO().start()
assert process.waitFor(30, java.util.concurrent.TimeUnit.SECONDS)
assert process.exitValue() == 23
c = new Collector(f.file); assert c.status().continuous_prefix == 1; c.close()

// Read-only status can run while the lock is held; stale or stopped state is
// not a healthy heartbeat, and the command must not rewrite the checkpoint.
f = fixture(); c = new Collector(f.file); c.reconcile(1, { payload(it) })
def statusCommand = {
    def statusProcess = new ProcessBuilder('java', '-cp', System.getProperty('java.class.path'), 'groovy.ui.GroovyMain',
        '/src/offline.groovy', 'status', f.file.toString()).redirectErrorStream(true).start()
    assert statusProcess.waitFor(30, java.util.concurrent.TimeUnit.SECONDS)
    [code: statusProcess.exitValue(), output: statusProcess.inputStream.text]
}
byte[] before = Collector.read(f.root.resolve('state/checkpoint.json'), Collector.STATE_LIMIT)
def status = statusCommand()
assert status.code == 0 && StrictJson.parse(status.output.getBytes('UTF-8')).continuous_prefix == 1
assert Arrays.equals(before, Collector.read(f.root.resolve('state/checkpoint.json'), Collector.STATE_LIMIT))
c.close(); assert statusCommand().code == 1
Map checkpoint = StrictJson.parse(Collector.read(f.root.resolve('state/checkpoint.json'), Collector.STATE_LIMIT))
checkpoint.lifecycle = 'running'; checkpoint.observed_at = '2020-01-01T00:00:00Z'
checkpoint.last_sweep_at = checkpoint.observed_at
Files.write(f.root.resolve('state/checkpoint.json'), Collector.encode(checkpoint))
status = statusCommand()
assert status.code == 1 && StrictJson.parse(status.output.getBytes('UTF-8')).stale

// Retention and proof are checked without changing state; an explicit retire
// keeps identity after deletion and suppresses historical regeneration.
Instant cleanupTime = Instant.now().minusSeconds(9 * 86400L)
Closure cleanupClock = { cleanupTime }
def acknowledgeOne = { Map ff, long number ->
    Path receipt = Files.createFile(ff.root.resolve("ack/build-${number}.json"), privateFile)
    Files.write(receipt, Collector.encode([version: 1, source_id: ff.config.binding.source_id, build_number: number,
        payload_sha256: Collector.digest(Collector.read(ff.root.resolve("input/build-${number}.json"), 16384))]))
}
f = fixture(cleanupClock); c = new Collector(f.file, false, null, cleanupClock)
c.reconcile(2, { payload(it) }); acknowledgeOne(f, 1); c.reconcile(2, { payload(it) })
before = Collector.read(f.root.resolve('state/checkpoint.json'), Collector.STATE_LIMIT)
assert Collector.cleanupPlan(f.config, Collector.checkpoint(f.config), 1, cleanupTime).reason == 'retention_not_met'
fails('collector_cleanup_not_eligible') { c.cleanup(1) }
assert c.status().lifecycle == 'running'
assert Arrays.equals(before, Collector.read(f.root.resolve('state/checkpoint.json'), Collector.STATE_LIMIT))
cleanupTime = cleanupTime.plusSeconds(7 * 86400L).minusNanos(1)
assert !Collector.cleanupPlan(f.config, Collector.checkpoint(f.config), 1, cleanupTime).eligible
cleanupTime = cleanupTime.plusNanos(1)
assert Collector.cleanupPlan(f.config, Collector.checkpoint(f.config), 1, cleanupTime).eligible
assert !Collector.cleanupPlan(f.config, Collector.checkpoint(f.config), 2, cleanupTime).eligible
byte[] savedInput = Collector.read(f.root.resolve('input/build-1.json'), 16384)
c.cleanup(1)
assert !Files.exists(f.root.resolve('input/build-1.json')) && Files.exists(f.root.resolve('input/build-2.json'))
assert Collector.checkpoint(f.config).version == 2 && c.status().counts.retired == 1 && c.status().continuous_prefix == 2
c.close(); c = new Collector(f.file, false, null, cleanupClock)
c.reconcile(2, { throw new IOException('history already pruned') })
c.finalized(1, 2, { payload(it) })
assert !Files.exists(f.root.resolve('input/build-1.json'))
Files.write(Files.createFile(f.root.resolve('input/build-1.json'), privateFile), savedInput)
c.reconcile(2, { payload(it) })
assert Files.exists(f.root.resolve('input/build-1.json')) // No automatic deletion on restore.
assert c.cleanup(1).already_retired
c.close()

// Copy a stopped post-cleanup backup to a new private directory. Its retired
// identity survives restore even if no Jenkins history remains at all.
def restored = fixture(cleanupClock)
['input', 'state', 'ack'].each { String part ->
    Files.newDirectoryStream(f.root.resolve(part)).withCloseable { stream ->
        for (Path source : stream) Files.copy(source, restored.root.resolve(part).resolve(source.fileName), StandardCopyOption.REPLACE_EXISTING)
    }
}
c = new Collector(restored.file, false, null, cleanupClock)
c.reconcile(2, { null }); assert c.status().counts.retired == 1 && !Files.exists(restored.root.resolve('input/build-1.json'))
fails('collector_input_conflict') { c.finalized(1, 2, { payload(it, 'FAILURE') }) }; c.close()

// Crash windows include intent publication, unlink and directory sync. Every
// restart either preserves the original payload or sees a durable retired ID.
['write', 'file_sync', 'rename', 'directory_sync', 'unlink', 'unlink_directory_sync'].each { String stage ->
    cleanupTime = Instant.now().minusSeconds(9 * 86400L)
    f = fixture(cleanupClock); c = new Collector(f.file, false, null, cleanupClock)
    c.reconcile(1, { payload(it) }); acknowledgeOne(f, 1); c.reconcile(1, { payload(it) }); c.close()
    cleanupTime = cleanupTime.plusSeconds(8 * 86400L)
    c = new Collector(f.file, false, { String at -> if (at == stage) throw new IOException('synthetic') }, cleanupClock)
    try { c.cleanup(1); assert false } catch (IOException expected) { }
    c.close()
    c = new Collector(f.file, false, null, cleanupClock)
    c.cleanup(1); assert c.status().counts.retired == 1 && !Files.exists(f.root.resolve('input/build-1.json'))
    c.close()
}

// Missing acknowledgment and conflicting input cannot be made eligible by age.
cleanupTime = Instant.now().minusSeconds(9 * 86400L)
f = fixture(cleanupClock); c = new Collector(f.file, false, null, cleanupClock)
c.reconcile(1, { payload(it) }); acknowledgeOne(f, 1); c.reconcile(1, { payload(it) }); c.close()
cleanupTime = cleanupTime.plusSeconds(8 * 86400L)
Path cleanupCrash = Files.createFile(f.root.resolve('cleanup-crash.groovy'), privateFile)
Files.write(cleanupCrash, '''
def core = new Collector(java.nio.file.Paths.get(args[0]), false,
    { String stage -> if (stage == 'unlink_directory_sync') Runtime.runtime.halt(24) },
    { java.time.Instant.parse(args[1]) })
core.cleanup(1)
'''.getBytes('UTF-8'))
def cleanupProcess = new ProcessBuilder('java', '-cp', System.getProperty('java.class.path'), 'groovy.ui.GroovyMain',
    cleanupCrash.toString(), f.file.toString(), cleanupTime.toString()).inheritIO().start()
assert cleanupProcess.waitFor(30, java.util.concurrent.TimeUnit.SECONDS) && cleanupProcess.exitValue() == 24
c = new Collector(f.file, false, null, cleanupClock)
assert c.cleanup(1).already_retired && !Files.exists(f.root.resolve('input/build-1.json')); c.close()

// Missing acknowledgment and conflicting input cannot be made eligible by age.
cleanupTime = Instant.now().minusSeconds(9 * 86400L)
f = fixture(cleanupClock); c = new Collector(f.file, false, null, cleanupClock)
c.reconcile(1, { payload(it) }); acknowledgeOne(f, 1); c.reconcile(1, { payload(it) })
cleanupTime = cleanupTime.plusSeconds(8 * 86400L)
Files.delete(f.root.resolve('ack/build-1.json'))
assert Collector.cleanupPlan(f.config, Collector.checkpoint(f.config), 1, cleanupTime).reason == 'ack_required'
acknowledgeOne(f, 1)
Files.write(f.root.resolve('input/build-1.json'), Collector.encode(payload(1, 'FAILURE')))
fails('collector_input_conflict') { c.cleanup(1) }
assert Files.exists(f.root.resolve('input/build-1.json')); c.close()

// Optional synthetic contract export. This directory is transport for tests,
// never a supported durable spool (host Docker mounts may be FUSE).
if (System.getenv('COLLECTOR_CONTRACT_EXPORT')) {
    Path destination = Paths.get(System.getenv('COLLECTOR_CONTRACT_EXPORT'))
    cleanupTime = Instant.now().minusSeconds(9 * 86400L)
    f = fixture(cleanupClock); c = new Collector(f.file, false, null, cleanupClock)
    c.reconcile(5, { payload(it, ['SUCCESS','FAILURE','ABORTED','UNSTABLE','NOT_BUILT'][(int) it - 1]) })
    for (int n = 1; n <= 5; n++) {
        Files.copy(f.root.resolve("input/build-${n}.json"), destination.resolve("build-${n}.json"))
    }
    acknowledgeOne(f, 1); c.reconcile(5, { payload(it) })
    cleanupTime = cleanupTime.plusSeconds(8 * 86400L)
    c.cleanup(1); c.close()
    Files.copy(f.root.resolve('state/manifest.json'), destination.resolve('collector-manifest.json'))
    Files.copy(f.root.resolve('state/checkpoint.json'), destination.resolve('collector-checkpoint.json'))
}
println 'collector_checks_passed'
