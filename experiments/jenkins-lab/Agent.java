import java.nio.file.Files;
import java.nio.file.Path;
import hudson.remoting.Launcher;

// Test-only bootstrap: the secret is read in the JVM, never placed in OS argv.
class Agent {
    public static void main(String[] args) throws Exception {
        String secret = Files.readString(Path.of("/tmp/nexus-agent-secret")).trim();
        if (!secret.matches("[0-9a-f]{64}")) {
            throw new IllegalStateException("invalid lab agent credential");
        }
        Launcher.main(new String[] {
            "-url", "http://controller:8080/", "-name", "nexus-lab-agent",
            "-secret", secret, "-webSocket", "-workDir", "/home/jenkins/agent",
            "-noReconnectAfter", "5m"
        });
    }
}
