/**
 * CloudNative Supabase - Dagger module for building operator container image
 *
 * Builds the Go binary and packages it into a distroless container,
 * matching the existing Dockerfile pattern.
 */
import { dag, Container, Directory, object, func } from "@dagger.io/dagger";

const GO_VERSION = "1.25";
const DISTROLESS_IMAGE = "gcr.io/distroless/static:nonroot";

@object()
export class CloudnativeSupabase {
  /**
   * Build the operator binary and package into a distroless container
   *
   * @param source - Root directory of the Go project
   */
  @func()
  build(source: Directory): Container {
    // Build stage: compile Go binary with caching
    const builder = dag
      .container()
      .from(`golang:${GO_VERSION}`)
      .withMountedDirectory("/workspace", source)
      .withWorkdir("/workspace")
      .withMountedCache("/go/pkg/mod", dag.cacheVolume("go-mod"))
      .withMountedCache("/root/.cache/go-build", dag.cacheVolume("go-build"))
      .withEnvVariable("CGO_ENABLED", "0")
      .withEnvVariable("GOOS", "linux")
      .withEnvVariable("GOARCH", "amd64")
      .withExec(["go", "mod", "download"])
      .withExec(["go", "build", "-a", "-o", "manager", "cmd/main.go"]);

    // Runtime stage: distroless non-root
    return dag
      .container()
      .from(DISTROLESS_IMAGE)
      .withFile("/manager", builder.file("/workspace/manager"))
      .withEntrypoint(["/manager"])
      .withUser("65532:65532");
  }

  /**
   * Build and publish to a registry
   *
   * @param source - Root directory of the Go project
   * @param registry - Registry URL (default: ttl.sh for testing)
   * @param image - Image name
   * @param tag - Image tag
   */
  @func()
  async publish(
    source: Directory,
    registry: string = "ttl.sh",
    image: string = "cloudnative-supabase",
    tag: string = "latest"
  ): Promise<string> {
    const container = this.build(source);
    const ref = `${registry}/${image}:${tag}`;
    return container.publish(ref);
  }

  /**
   * Run unit tests
   *
   * @param source - Root directory of the Go project
   */
  @func()
  async test(source: Directory): Promise<string> {
    return dag
      .container()
      .from(`golang:${GO_VERSION}`)
      .withMountedDirectory("/workspace", source)
      .withWorkdir("/workspace")
      .withMountedCache("/go/pkg/mod", dag.cacheVolume("go-mod"))
      .withMountedCache("/root/.cache/go-build", dag.cacheVolume("go-build"))
      .withExec([
        "go",
        "test",
        "./pkg/...",
        "./internal/resources/...",
        "-v",
        "-count=1",
      ])
      .stdout();
  }
}
