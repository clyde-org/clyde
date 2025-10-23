```mermaid
sequenceDiagram
    participant Client as Client (ctr/k8s)
    participant Containerd as Containerd
    participant Clyde as Clyde (P2P Proxy)
    participant P2P as P2P Network
    participant Remote as Remote Registry
    participant Apull as Apull (overlayfs)

    Client->>Containerd: PullImage("alpine:latest")
    Containerd->>Clyde: Resolve image (name → manifest)

    alt Cached in Clyde/P2P
        Clyde-->>Containerd: Return manifest + layer digests
    else Cache Miss
        Clyde->>Remote: Fetch manifest
        Remote-->>Clyde: Manifest + layer metadata
        Clyde-->>Containerd: Return manifest + layer digests
    end

    Containerd->>Clyde: Get layers (by digest)
    Clyde->>P2P: Attempt P2P layer download

    alt P2P Hit
        P2P-->>Clyde: Layer blobs (from peers)
    else P2P Miss
        Clyde->>Remote: Pull layer blobs
        Remote-->>Clyde: Layer blobs (from registry)
    end

    Clyde-->>Containerd: Compressed layer data
    Containerd->>Apull: Prepare snapshot (Apull init)
    Apull->>Containerd: Empty overlayfs mount

    loop For each layer
        Containerd->>Apull: Apull applies layer (overlayfs merge)
    end

    Apull-->>Containerd: Final rootfs (overlayfs merged)
    Containerd-->>Client: Image ready
