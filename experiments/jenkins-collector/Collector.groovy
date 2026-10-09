import groovy.json.JsonOutput
import java.nio.ByteBuffer
import java.nio.channels.FileChannel
import java.nio.channels.FileLock
import java.nio.file.*
import java.nio.file.attribute.PosixFilePermissions
import java.security.MessageDigest
import java.time.Instant

// Local trusted controller code. No network, credential or Jenkins dependency.
// Directories are private to one deployment UID; agents must never mount them.
class Collector implements Closeable {
    static final int LIMIT = 10000
    static final int BATCH = 100
    static final int PAYLOAD_LIMIT = 16384
    static final int STATE_LIMIT = 4 * 1024 * 1024
    static final List BINDING_KEYS = ['version', 'receiver_id', 'jenkins_id', 'origin', 'source_id',
        'workspace_id', 'component_id', 'job_full_name', 'first_build_number']
    static final def PRIVATE_FILE = PosixFilePermissions.fromString('rw-------')
    static final def PRIVATE_DIR = PosixFilePermissions.fromString('rwx------')
    final Map config
    final Path input, stateDir, ack
    private FileChannel lockChannel
    private FileLock lock
    private Map state
    private boolean failed = false
    private boolean closed = false
    private Closure fault

    static void require(boolean valid, String code = 'collector_invalid_state') {
        if (!valid) throw new CollectorFailure(code)
    }
    static void keys(Object value, List keys) {
        require(value instanceof Map && value.keySet() == keys.toSet())
    }
    static boolean integer(Object value) { value instanceof Integer || value instanceof Long || value instanceof BigInteger }
    static byte[] encode(Object value) { JsonOutput.toJson(value).getBytes('UTF-8') }
    static String digest(byte[] bytes) { MessageDigest.getInstance('SHA-256').digest(bytes).encodeHex().toString() }
    static String now() { Instant.now().toString() }
    static String name(long n) { "build-${n}.json" }
    static void directory(Path path) {
        require(path.isAbsolute() && path.normalize() == path && path.toRealPath() == path, 'collector_unsafe_directory')
        require(Files.isDirectory(path, LinkOption.NOFOLLOW_LINKS) && Files.getPosixFilePermissions(path) == PRIVATE_DIR,
            'collector_unsafe_directory')
        // Fixed Linux JDK deployment only; remote / unverified filesystem fails closed.
        require(Files.getFileStore(path).type() in ['ext4', 'xfs', 'btrfs', 'tmpfs', 'overlay'], 'collector_unsupported_filesystem')
        require(Files.getOwner(path).name == System.getProperty('user.name'), 'collector_unsafe_directory')
    }
    static void regular(Path path, long limit) {
        def a = Files.readAttributes(path, 'unix:*', LinkOption.NOFOLLOW_LINKS)
        require(a.isRegularFile && a.nlink == 1 && a.size <= limit && a.permissions == PRIVATE_FILE &&
            a.owner.name == System.getProperty('user.name'), 'collector_unsafe_file')
    }
    static byte[] read(Path path, int limit) {
        regular(path, limit)
        FileChannel channel = FileChannel.open(path, StandardOpenOption.READ, LinkOption.NOFOLLOW_LINKS)
        try {
            ByteBuffer buffer = ByteBuffer.allocate(limit + 1)
            while (buffer.hasRemaining() && channel.read(buffer) != -1) { }
            require(buffer.position() <= limit, 'collector_capacity')
            return Arrays.copyOf(buffer.array(), buffer.position())
        } finally { channel.close() }
    }
    static Map loadConfig(Path path) {
        Map c = (Map) StrictJson.parse(read(path, 16384))
        keys(c, ['version', 'input_dir', 'state_dir', 'ack_dir', 'binding'])
        require(c.version == 1 && integer(c.version), 'collector_invalid_config')
        keys(c.binding, BINDING_KEYS)
        Map b = c.binding
        require(b.version == 1 && integer(b.version) && integer(b.first_build_number) &&
            b.first_build_number >= 1 && b.first_build_number <= 2147483647, 'collector_invalid_config')
        ['receiver_id', 'jenkins_id', 'source_id'].each {
            require(b[it] instanceof String && b[it] ==~ /[A-Za-z][A-Za-z0-9_-]{0,63}/, 'collector_invalid_config')
        }
        require(b.workspace_id instanceof String && b.workspace_id ==~ /wrk_[A-Za-z0-9_-]+/, 'collector_invalid_config')
        require(b.component_id instanceof String && b.component_id ==~ /cmp_[A-Za-z0-9_-]+/, 'collector_invalid_config')
        require(b.job_full_name instanceof String && b.job_full_name.trim() == b.job_full_name &&
            b.job_full_name.getBytes('UTF-8').length in 1..255 &&
            !b.job_full_name.codePoints().anyMatch { int p -> Character.isISOControl(p) || p == 65533 || p in 0xD800..0xDFFF }, 'collector_invalid_config')
        URI origin = new URI(b.origin as String)
        require(origin.scheme == 'https' && origin.host && origin.rawUserInfo == null &&
            origin.rawQuery == null && origin.rawFragment == null && !origin.rawPath, 'collector_invalid_config')
        List<Path> dirs = ['input_dir', 'state_dir', 'ack_dir'].collect {
            require(c[it] instanceof String, 'collector_invalid_config')
            Path p = Paths.get(c[it]); directory(p); p
        }
        dirs.eachWithIndex { Path p, int i ->
            dirs.eachWithIndex { Path q, int j -> require(i == j || !p.startsWith(q), 'collector_invalid_config') }
            require(!path.startsWith(p), 'collector_invalid_config')
        }
        c
    }
    Collector(Path configPath, boolean initialize = false, Closure fault = null) {
        this.config = loadConfig(configPath)
        input = Paths.get(config.input_dir); stateDir = Paths.get(config.state_dir); ack = Paths.get(config.ack_dir)
        this.fault = fault
        Path lockPath = stateDir.resolve('.lock')
        lockChannel = FileChannel.open(lockPath, [StandardOpenOption.CREATE, StandardOpenOption.WRITE, LinkOption.NOFOLLOW_LINKS] as Set,
            PosixFilePermissions.asFileAttribute(PRIVATE_FILE))
        try {
            regular(lockPath, 0)
            try { lock = lockChannel.tryLock() } catch (java.nio.channels.OverlappingFileLockException ignored) { }
            require(lock != null, 'collector_locked')
            Path manifest = stateDir.resolve('manifest.json')
            if (initialize) {
                require(!Files.exists(manifest, LinkOption.NOFOLLOW_LINKS), 'collector_already_initialized')
                require(names(stateDir).keySet() == ['.lock'].toSet() && names(input).isEmpty() && names(ack).isEmpty(),
                    'collector_refuses_adoption')
                write(manifest, encode(config.binding))
                long first = config.binding.first_build_number
                state = [version: 1, binding: config.binding, prefix: first - 1, high: first - 1, scan_high: first - 1, cursor: first,
                    observed_at: now(), last_sweep_at: null, lifecycle: 'stopped', error: '', entries: [:]]
                save()
            } else {
                require(StrictJson.parse(read(manifest, 16384)) == config.binding, 'collector_binding_mismatch')
                state = (Map) StrictJson.parse(read(stateDir.resolve('checkpoint.json'), STATE_LIMIT))
                validateState(state, config)
            }
            inventory()
        } catch (Throwable error) {
            lock?.release(); lockChannel.close()
            throw error
        }
    }
    static void validateState(Map state, Map config) {
        keys(state, ['version', 'binding', 'prefix', 'high', 'scan_high', 'cursor', 'observed_at', 'last_sweep_at', 'lifecycle', 'error', 'entries'])
        require(state.version == 1 && state.binding == config.binding, 'collector_binding_mismatch')
        long first = config.binding.first_build_number
        require(integer(state.prefix) && integer(state.high) && integer(state.scan_high) && integer(state.cursor) && state.prefix >= first - 1 &&
            state.prefix <= state.high && state.high <= 2147483647 && state.high - first < LIMIT &&
            state.scan_high >= first - 1 && state.scan_high <= state.high && state.cursor >= first && state.cursor <= state.scan_high + 1)
        require(state.lifecycle in ['running', 'stopped', 'paused'] && state.error instanceof String &&
            (state.error == '' || state.error in CollectorFailure.CODES))
        Instant.parse(state.observed_at as String)
        if (state.last_sweep_at != null) Instant.parse(state.last_sweep_at as String)
        require(state.entries instanceof Map && state.entries.size() <= LIMIT)
        state.entries.each { String k, Object value ->
            require(k ==~ /[1-9][0-9]{0,9}/ && k.toLong() >= first && k.toLong() <= state.high)
            keys(value, ['kind', 'digest', 'handoff_at'])
            require(value.kind in ['published', 'running', 'missing', 'unreadable'])
            require(value.kind == 'published' ? value.digest instanceof String && value.digest ==~ /[0-9a-f]{64}/ : value.digest == '')
            if (value.handoff_at != null) {
                require(value.kind == 'published'); Instant.parse(value.handoff_at as String)
            }
        }
        require(state.prefix == prefix(state, config))
    }
    private Map names(Path dir) {
        directory(dir)
        Map result = [:]
        long bytes = 0
        Files.newDirectoryStream(dir).withCloseable { stream ->
            for (Path p : stream) {
                regular(p, STATE_LIMIT)
                result[p.fileName.toString()] = Files.size(p)
                bytes += Files.size(p)
                require(result.size() <= LIMIT + 4 && bytes <= 164 * 1024 * 1024, 'collector_capacity')
            }
        }
        result
    }
    private void inventory() {
        require(StrictJson.parse(read(stateDir.resolve('manifest.json'), 16384)) == config.binding, 'collector_binding_mismatch')
        [input, ack].each { Path dir ->
            def files = names(dir)
            require(files.size() <= LIMIT, 'collector_capacity')
            files.each { String n, long size ->
                if (n.startsWith('.pending-')) return
                require(n ==~ /build-[1-9][0-9]{0,9}\.json/)
                long number = n.substring(6, n.length() - 5).toLong()
                require(number >= config.binding.first_build_number && number <= 2147483647)
                require(size <= (dir == input ? PAYLOAD_LIMIT : 1024), 'collector_capacity')
            }
        }
        def files = names(stateDir)
        require(files.values().sum(0L) <= 8 * 1024 * 1024, 'collector_capacity')
        files.each { String n, long size -> require(n in ['.lock', 'manifest.json', 'checkpoint.json'] || n.startsWith('.pending-')) }
    }
    private void write(Path path, byte[] bytes) {
        require(bytes.length <= STATE_LIMIT, 'collector_capacity')
        Path temporary = Files.createTempFile(path.parent, '.pending-', '', PosixFilePermissions.asFileAttribute(PRIVATE_FILE))
        try {
            FileChannel file = FileChannel.open(temporary, StandardOpenOption.WRITE, LinkOption.NOFOLLOW_LINKS)
            try {
                fault?.call('write')
                ByteBuffer buffer = ByteBuffer.wrap(bytes)
                while (buffer.hasRemaining()) file.write(buffer)
                fault?.call('file_sync'); file.force(true)
            } finally { file.close() }
            fault?.call('rename')
            Files.move(temporary, path, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
            fault?.call('directory_sync')
            FileChannel directory = FileChannel.open(path.parent, StandardOpenOption.READ)
            try { directory.force(true) } finally { directory.close() }
        } finally { Files.deleteIfExists(temporary) }
    }
    private static long prefix(Map state, Map config) {
        long n = config.binding.first_build_number
        while (state.entries[Long.toString(n)]?.kind == 'published') n++
        n - 1
    }
    private void save() {
        state.prefix = prefix(state, config); state.observed_at = now()
        write(stateDir.resolve('checkpoint.json'), encode(state))
    }
    private void healthy() { require(!closed && !failed, 'collector_paused') }
    private void pause(Throwable error) {
        failed = true
        state.lifecycle = 'paused'; state.error = safeCode(error)
        try { save() } catch (Throwable ignored) { /* Disk may be unavailable; caller emits safeCode. */ }
    }
    static String safeCode(Throwable error) { error instanceof CollectorFailure ? error.message : 'collector_io_failed' }
    synchronized void reportFailure(Throwable error) { if (!closed && !failed) pause(error) }

    // snapshot is null for a missing historical run, [pending:true] while active,
    // or the exact whitelist payload from the Jenkins adapter.
    private void visit(long number, Closure lookup, boolean verifyExisting = false) {
        String key = Long.toString(number)
        Map previous = state.entries[key]
        Path path = input.resolve(name(number))
        byte[] bytes = null
        if (Files.exists(path, LinkOption.NOFOLLOW_LINKS)) bytes = read(path, PAYLOAD_LIMIT)
        if (previous?.kind == 'published' && bytes != null && !verifyExisting) {
            require(digest(bytes) == previous.digest, 'collector_input_conflict')
            acknowledge(number, previous)
            return
        }
        def snapshot
        try { snapshot = lookup(number) } catch (Throwable ignored) {
            // Do not erase a prior immutable identity when historical reading fails.
            if (previous?.kind == 'published') throw new CollectorFailure('collector_published_input_missing')
            state.entries[key] = [kind: 'unreadable', digest: '', handoff_at: null]
            return
        }
        if (snapshot == null || snapshot.pending == true) {
            if (previous?.kind == 'published') throw new CollectorFailure('collector_published_input_missing')
            require(bytes == null, 'collector_untracked_input')
            state.entries[key] = [kind: snapshot == null ? 'missing' : 'running', digest: '', handoff_at: null]
            return
        }
        keys(snapshot, ['version', 'job_full_name', 'build_number', 'building', 'in_progress', 'result', 'started_at', 'completed_at'])
        require(snapshot.version == 1 && snapshot.job_full_name == config.binding.job_full_name && snapshot.build_number == number &&
            snapshot.building == false && snapshot.in_progress == false && snapshot.result in ['SUCCESS', 'FAILURE', 'ABORTED', 'UNSTABLE', 'NOT_BUILT'],
            'collector_invalid_snapshot')
        Instant start = Instant.parse(snapshot.started_at as String), end = Instant.parse(snapshot.completed_at as String)
        require(!start.isAfter(end) && start.toEpochMilli() > 0 && end.toEpochMilli() <= System.currentTimeMillis() + 300000,
            'collector_invalid_snapshot')
        byte[] generated = encode(snapshot)
        require(generated.length <= PAYLOAD_LIMIT, 'collector_capacity')
        String sha = digest(generated)
        require(previous?.kind != 'published' || sha == previous.digest, 'collector_input_conflict')
        require(bytes == null || Arrays.equals(bytes, generated), 'collector_input_conflict')
        if (bytes == null) {
            require(names(input).size() < LIMIT, 'collector_capacity')
            write(path, generated)
        }
        state.entries[key] = [kind: 'published', digest: sha, handoff_at: previous?.handoff_at]
        acknowledge(number, state.entries[key])
    }
    private void acknowledge(long number, Map entry) {
        Path path = ack.resolve(name(number))
        if (!Files.exists(path, LinkOption.NOFOLLOW_LINKS)) return
        Map a = (Map) StrictJson.parse(read(path, 1024))
        keys(a, ['version', 'source_id', 'build_number', 'payload_sha256'])
        require(a.version == 1 && a.source_id == config.binding.source_id && a.build_number == number &&
            a.payload_sha256 == entry.digest, 'collector_ack_conflict')
        if (entry.handoff_at == null) entry.handoff_at = now()
    }
    synchronized void finalized(long number, long high, Closure lookup) {
        healthy()
        if (number < config.binding.first_build_number) return
        try {
            observeHigh(high); inventory()
            require(number <= high, 'collector_source_rewind')
            visit(number, lookup, true); state.lifecycle = 'running'; state.error = ''; save()
        } catch (Throwable error) { pause(error); throw error }
    }
    private void observeHigh(long high) {
        require(high >= state.high && high <= 2147483647, 'collector_source_rewind')
        require(high - config.binding.first_build_number < LIMIT, 'collector_capacity')
        state.high = high
    }
    synchronized void reconcile(long high, Closure lookup) {
        healthy()
        try {
            observeHigh(high); inventory()
            int visited = 0
            // Freeze each sweep's upper bound across batches. Continuous new
            // builds cannot postpone revisiting an older missing callback forever.
            if (state.cursor == config.binding.first_build_number) state.scan_high = high
            while (state.cursor <= state.scan_high && visited < BATCH) {
                visit((long) state.cursor, lookup); state.cursor++; visited++
            }
            if (state.cursor > state.scan_high) {
                state.cursor = config.binding.first_build_number
                state.last_sweep_at = now()
            }
            state.lifecycle = 'running'; state.error = ''; save()
        } catch (Throwable error) { pause(error); throw error }
    }
    synchronized Map status() {
        Map counts = [published: 0, running: 0, missing: 0, unreadable: 0, acknowledged: 0]
        state.entries.values().each { counts[it.kind]++; if (it.handoff_at != null) counts.acknowledged++ }
        [version: 1, source_id: config.binding.source_id, observed_at: state.observed_at,
            last_sweep_at: state.last_sweep_at, lifecycle: state.lifecycle, error: state.error,
            continuous_prefix: state.prefix, captured_high: state.high, scan_high: state.scan_high, next_scan: state.cursor,
            unscanned: state.high - config.binding.first_build_number + 1 - state.entries.size(), counts: counts,
            cleanup_enabled: false]
    }
    synchronized void close() {
        if (closed) return
        try {
            if (!failed) { state.lifecycle = 'stopped'; save() }
        } finally {
            closed = true
            try { lock?.release() } finally { lockChannel.close() }
        }
    }
}

class CollectorFailure extends IOException {
    static final Set CODES = ['collector_invalid_state', 'collector_invalid_config', 'collector_unsafe_directory',
        'collector_unsupported_filesystem', 'collector_unsafe_file', 'collector_capacity', 'collector_locked',
        'collector_already_initialized', 'collector_refuses_adoption', 'collector_binding_mismatch', 'collector_paused',
        'collector_io_failed', 'collector_input_conflict', 'collector_published_input_missing', 'collector_untracked_input',
        'collector_invalid_snapshot', 'collector_ack_conflict', 'collector_source_rewind', 'collector_job_unavailable'].toSet()
    CollectorFailure(String code) { super(code); if (!CODES.contains(code)) throw new IllegalArgumentException('invalid_code') }
}
