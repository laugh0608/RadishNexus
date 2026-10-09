// Copy these four Groovy files into /opt/radishnexus/collector (read-only mount).
// Only this entry point belongs in init.groovy.d. No user/job/security mutation.
import jenkins.model.Jenkins
import java.nio.file.Paths

def loader = new GroovyClassLoader(this.class.classLoader)
['StrictJson.groovy', 'Collector.groovy', 'JenkinsCollector.groovy'].each {
    loader.parseClass(new File('/opt/radishnexus/collector', it))
}
try {
    loader.loadClass('JenkinsCollector').install(Jenkins.get(), Paths.get('/opt/radishnexus/collector-config.json'))
    println 'collector_registered'
} catch (Throwable error) {
    // Reflection/Groovy wrappers can contain paths or objects. Never print them.
    println 'collector_start_failed'
    throw new IllegalStateException('collector_start_failed')
}
