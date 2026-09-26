// Disposable test instance only. No repository checkout, plugins or Nexus key.
import hudson.model.*
import hudson.model.listeners.RunListener
import hudson.security.FullControlOnceLoggedInAuthorizationStrategy
import hudson.security.HudsonPrivateSecurityRealm
import hudson.security.csrf.DefaultCrumbIssuer
import hudson.slaves.DumbSlave
import hudson.slaves.JNLPLauncher
import hudson.slaves.RetentionStrategy
import hudson.tasks.Shell
import jenkins.model.Jenkins
import net.sf.json.JSONObject
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.nio.file.attribute.PosixFilePermissions
import java.security.SecureRandom
import java.time.Instant
import java.util.concurrent.TimeUnit

Jenkins instance = Jenkins.get()
File state = new File(instance.rootDir, 'nexus-lab')
if (state.exists()) {
    throw new IllegalStateException('lab requires a fresh test volume; old state was preserved')
}
Files.createDirectory(state.toPath(), PosixFilePermissions.asFileAttribute(PosixFilePermissions.fromString('rwx------')))
def privateWrite = { String name, String value ->
    def file = new File(state, name).toPath()
    Files.createFile(file, PosixFilePermissions.asFileAttribute(PosixFilePermissions.fromString('rw-------')))
    Files.writeString(file, value)
}
byte[] random = new byte[32]
new SecureRandom().nextBytes(random)
String password = Base64.urlEncoder.withoutPadding().encodeToString(random)
def realm = new HudsonPrivateSecurityRealm(false)
realm.createAccount('lab-admin', password)
instance.securityRealm = realm
def strategy = new FullControlOnceLoggedInAuthorizationStrategy()
strategy.setAllowAnonymousRead(false)
instance.authorizationStrategy = strategy
instance.crumbIssuer = new DefaultCrumbIssuer(true)
instance.numExecutors = 0
instance.slaveAgentPort = -1
privateWrite('admin-password', password)

def agent = new DumbSlave('nexus-lab-agent', '/home/jenkins/agent', new JNLPLauncher())
agent.numExecutors = 1
agent.labelString = 'nexus-lab-agent'
agent.mode = Node.Mode.EXCLUSIVE
agent.retentionStrategy = new RetentionStrategy.Always()
instance.addNode(agent)
privateWrite('agent-secret', agent.toComputer().getJnlpMac())

def job = instance.createProject(FreeStyleProject.class, 'nexus-ci-probe')
job.assignedLabel = instance.getLabel('nexus-lab-agent')
job.concurrentBuild = false
job.addProperty(new ParametersDefinitionProperty(new StringParameterDefinition('OUTCOME', 'SUCCESS')))
job.buildersList.add(new Shell('''set -eu
case "$OUTCOME" in
  SUCCESS) exit 0 ;;
  FAILURE) exit 1 ;;
  ABORTED) sleep 120 ;;
  *) exit 2 ;;
esac
'''))
job.save()
instance.save()

// onFinalized runs after Jenkins has stored the terminal run. Snapshots are
// controller-authored and never writable by the build agent.
class NexusLabFinalized extends RunListener<Run> {
    private final File state
    NexusLabFinalized(File state) { super(Run.class); this.state = state }
    @Override void onFinalized(Run run) {
        if (run.parent.fullName != 'nexus-ci-probe') return
        if (run.isBuilding() || run.isInProgress() || run.result == null) {
            throw new IllegalStateException('lab run is not finalized')
        }
        long start = run.getStartTimeInMillis()
        long duration = run.getDuration()
        if (start <= 0 || duration < 0) throw new IllegalStateException('invalid lab run time')
        def payload = [version: 1, job_full_name: run.parent.fullName,
            build_number: run.number, building: false, in_progress: false,
            result: run.result.toString(), started_at: Instant.ofEpochMilli(start).toString(),
            completed_at: Instant.ofEpochMilli(Math.addExact(start, duration)).toString()]
        def temporary = new File(state, "build-${run.number}.json.tmp").toPath()
        Files.writeString(temporary, JSONObject.fromObject(payload).toString())
        Files.move(temporary, new File(state, "build-${run.number}.json").toPath(), StandardCopyOption.ATOMIC_MOVE)
    }
}
RunListener.all().add(new NexusLabFinalized(state))
privateWrite('ready', 'ready\n')

// Bound the probe to three synthetic jobs. No current-job post hook declares
// completion; the listener above is the only author of delivery input files.
Thread.startDaemon('nexus-lab-probes') {
    try {
        long deadline = System.nanoTime() + TimeUnit.MINUTES.toNanos(3)
        while (!agent.toComputer().online) {
            if (System.nanoTime() > deadline) throw new IllegalStateException('agent timeout')
            Thread.sleep(500)
        }
        for (String outcome : ['SUCCESS', 'FAILURE', 'ABORTED']) {
            def future = job.scheduleBuild2(0, new Cause.RemoteCause('local-lab', 'bounded integration probe'),
                new ParametersAction(new StringParameterValue('OUTCOME', outcome)))
            if (future == null) throw new IllegalStateException('probe not queued')
            def run = future.startCondition.get(30, TimeUnit.SECONDS)
            if (outcome == 'ABORTED') {
                Thread.sleep(1500)
                def executor = run.executor
                if (executor == null) throw new IllegalStateException('cancel target missing')
                executor.interrupt(Result.ABORTED)
            }
            future.get(60, TimeUnit.SECONDS)
            long finalizedDeadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(15)
            File snapshot = new File(state, "build-${run.number}.json")
            while (!snapshot.isFile()) {
                if (System.nanoTime() > finalizedDeadline) throw new IllegalStateException('snapshot timeout')
                Thread.sleep(100)
            }
            if (run.result.toString() != outcome) throw new IllegalStateException('unexpected probe result')
        }
        privateWrite('complete', 'complete\n')
    } catch (Throwable failure) {
        // Preserve a safe diagnostic category; do not dump credential-bearing objects.
        privateWrite('failed', failure.class.simpleName + '\n')
    }
}
