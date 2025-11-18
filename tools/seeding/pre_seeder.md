```mermaid
sequenceDiagram
    actor User
    participant Harbor
    participant K8sAPI
    participant "Cluster Nodes" as Cluster
    participant SeederNodes
    participant RegularNodes

    Note over SeederNodes,RegularNodes: Cluster Nodes Group
    User->>K8sAPI: 1. Label x% Nodes as Seeders
    K8sAPI->>SeederNodes: Apply 'p2p-seeder=true' label
    
    User->>K8sAPI: 2. Create Preload Job
    K8sAPI->>SeederNodes: Schedule Job Pods
    SeederNodes->>Harbor: 3. Pull Images
    Harbor-->>SeederNodes: Image Layers
    Note right of SeederNodes: Jobs auto-terminate after pull
    
    User->>K8sAPI: 4. Deploy Main DaemonSet
    K8sAPI->>SeederNodes: Schedule Pods
    K8sAPI->>RegularNodes: Schedule Pods
    
    RegularNodes->>SeederNodes: 5. P2P Transfer
    SeederNodes-->>RegularNodes: Image Chunks
    
    loop Health Monitor
        K8sAPI->>SeederNodes: Check Status
        alt Node Down
            K8sAPI->>K8sAPI: Relabel Replacement
        end
    end