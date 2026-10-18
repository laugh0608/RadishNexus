import hudson.init.InitMilestone
import hudson.model.Job
import hudson.model.Run
import hudson.model.listeners.RunListener
import jenkins.model.Jenkins
import java.nio.file.Path
import java.time.Instant
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.TimeUnit
import java.util.logging.Logger

// Install only from a deployment-controlled directory, never from a checkout.
class JenkinsCollector extends RunListener<Run> implements Closeable {
    static final String ID = 'radishnexus-collector-v1'
    private static final Logger LOG = Logger.getLogger('radishnexus.collector')
    private final Collector collector
    private final Jenkins instance
    private ScheduledExecutorService timer
    private boolean closed = false

    JenkinsCollector(Jenkins instance, Collector collector) {
        super(Run.class)
        this.instance = instance; this.collector = collector
    }
    String getCollectorID() { ID }
    static JenkinsCollector install(Jenkins instance, Path config) {
        synchronized (instance) {
            // A previous Groovy classloader has different class identity. Match
            // the reserved class name too; unknown implementations fail closed.
            Collector.require(!RunListener.all().any { it.class.name == JenkinsCollector.name }, 'collector_locked')
            Collector core = new Collector(config)
            JenkinsCollector listener = new JenkinsCollector(instance, core)
            try {
                RunListener.all().add(listener)
                listener.timer = Executors.newSingleThreadScheduledExecutor({ Runnable task ->
                    Thread t = new Thread(task, 'radishnexus-collector'); t.daemon = true; t
                } as java.util.concurrent.ThreadFactory)
                listener.timer.scheduleWithFixedDelay({ listener.tick() } as Runnable, 0, 60, TimeUnit.SECONDS)
                return listener
            } catch (Throwable error) {
                RunListener.all().remove(listener); listener.close(); throw error
            }
        }
    }
    private Job job() {
        def job = instance.getItemByFullName(collector.config.binding.job_full_name as String)
        Collector.require(job instanceof Job, 'collector_job_unavailable')
        (Job) job
    }
    static Map snapshot(Run run) {
        if (run == null) return null
        if (run.isBuilding() || run.isInProgress() || run.result == null) return [pending: true]
        long start = run.getStartTimeInMillis(), duration = run.duration
        Collector.require(start > 0 && duration >= 0, 'collector_invalid_snapshot')
        [version: 1, job_full_name: run.parent.fullName, build_number: run.number, building: false, in_progress: false,
            result: run.result.toString(), started_at: Instant.ofEpochMilli(start).toString(),
            completed_at: Instant.ofEpochMilli(Math.addExact(start, duration)).toString()]
    }
    private synchronized void tick() {
        if (closed || instance.initLevel != InitMilestone.COMPLETED) return
        try {
            Job job = job()
            collector.reconcile((long) job.nextBuildNumber - 1, { long n -> snapshot(job.getBuildByNumber((int) n)) })
        } catch (Throwable error) { collector.reportFailure(error); LOG.warning(Collector.safeCode(error)) }
    }
    @Override synchronized void onFinalized(Run run) {
        if (closed || instance.initLevel != InitMilestone.COMPLETED ||
            run.parent.fullName != collector.config.binding.job_full_name) return
        try {
            collector.finalized(run.number, (long) run.parent.nextBuildNumber - 1, { long n -> snapshot(run) })
        } catch (Throwable error) { collector.reportFailure(error); LOG.warning(Collector.safeCode(error)) }
    }
    synchronized Map status() { collector.status() }
    @Override synchronized void close() {
        if (closed) return
        closed = true
        timer?.shutdownNow()
        try { collector.close() } finally { RunListener.all().remove(this) }
    }
}
