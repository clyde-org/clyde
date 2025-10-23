```mermaid
flowchart TD
    A[User runs: pip install torch] --> B[Query PyPI Index /simple/torch/]

    subgraph PyPI [PyPI Ecosystem]
        direction LR
        I[Index: /simple/torch/]
        I --> M1[Link: torch-2.3.0-cp312-... whl]
        I --> M2[Link: torch-2.3.0.tar.gz]
        I --> M3[Link: torch-2.2.1-...]
    end

    B --HTTP Request--> I

    subgraph FetchMetadata [Phase 1: Find Best Match & Metadata]
        direction TB
        C[Download all package links & versions]
        C --> D{Is a compatible wheel (.whl) available?}
        D -- Yes --> E[Download .whl file]
        D -- No --> F[Download source .tar.gz]
        E --> G[Extract metadata from wheel metadata file METADATA.json/PKG-INFO]
        F --> H[Build wheel to get metadata requires setup.py/pyproject.toml]
        G --> J[Collect dependency list from metadata Requires-Dist]
        H --> J
    end

    J --> K

    subgraph ResolveDeps [Phase 2: Dependency Resolution]
        K[Add initial dependency e.g. torch==2.3.0] --> L[Fetch metadata for each new dependency e.g. filelock, typing-extensions]
        L --> M[Repeat recursively until full dependency tree is built]
        M --> N[Resolver finds a compatible set of package versions]
    end

    N --> O[Phase 3: Download & Install]

    subgraph DownloadInstall [Download & Install]
        direction LR
        P[Download all required .whl files from PyPI CDN] --> Q[Install each wheel: Unpack to site-packages/]
        Q --> R[Record installed files in INSTALLER file]
        R --> S[Generate .dist-info directory for metadata]
    end

    O --> P
    S --> T[Installation Complete!]

    style PyPI fill:#e1f5fe
    style FetchMetadata fill:#f3e5f5
    style ResolveDeps fill:#fff3e0
    style DownloadInstall fill:#e8f5e9
