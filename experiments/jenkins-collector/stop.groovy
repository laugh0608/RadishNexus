// Trusted administrator Script Console operation. Stops and detaches this
// exact implementation; it does not delete any file or revoke receiver access.
import hudson.model.listeners.RunListener
import jenkins.model.Jenkins

synchronized (Jenkins.get()) {
    def matches = RunListener.all().findAll { it.class.name == 'JenkinsCollector' }
    if (matches.size() != 1 || matches[0].collectorID != 'radishnexus-collector-v1') {
        throw new IllegalStateException('collector_identity_unknown')
    }
    matches[0].close()
    println 'collector_stopped'
}
