```mermaid
flowchart TD
    A[Start: Simulate Registry Load] --> B{"P2P"}
    B -->|Yes| C["Estimate Fallback Nodes
    N' = 1-5% of N"]
    B -->|No| D["All Nodes Use Registry
    N' = N"]
    C --> E["Test Bandwidth = N'/N × Actual
    (e.g., 1% if N'=10, N=1000)"]
    D --> E
    E --> F[Run Test with Scaled Bandwidth]
    F --> G{"Registry Load Realistic?"}
    G -->|No| H[Adjust N' or P2P Reliability]
    G -->|Yes| I[Testing Complete]

    style B fill:#ffeb99,stroke:#ff9900
    style C fill:#d4edda,stroke:#28a745
    style D fill:#f8d7da,stroke:#dc3545
    style G fill:#ffeb99,stroke:#ff9900