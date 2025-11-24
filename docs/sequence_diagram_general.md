```mermaid
sequenceDiagram
    participant Client as Client (e.g. pip, ctr, git)
    participant Service as Local Runtime (e.g. pip, containerd, git)
    participant Proxy as Clyde Proxy (P2P)
    participant P2P as P2P Network
    participant Remote as Remote Source (e.g. Registry, PyPI, Git Remote)

    Client->>Service: Request(data-id or package-name)
    Service->>Proxy: Resolve metadata (e.g. manifest, versions, digests)

    alt Cached in Proxy/P2P
        Proxy-->>Service: Return metadata (e.g. digests, versions)
    else Cache Miss
        Proxy->>Remote: Fetch metadata
        Remote-->>Proxy: Metadata response
        Proxy-->>Service: Return metadata
    end

    Service->>Proxy: Request data blob(s) by digest/version
    Proxy->>P2P: Attempt P2P download

    alt P2P Hit
        P2P-->>Proxy: Return data blob(s)
    else P2P Miss
        Proxy->>Remote: Fetch data blob(s)
        Remote-->>Proxy: Return data blob(s)
    end

    Proxy-->>Service: Deliver data blob(s)
    Service-->>Client: Data/package ready
