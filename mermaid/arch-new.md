```mermaid
flowchart TD
    subgraph Client
        C[Container Client\n(ctr/kubectl)]
    end

    subgraph Containerd
        CR[CRI Service]
        CS[Content Service]
        MS[Metadata Store]
        SS[Snapshotter Service]
    end

    subgraph Storage
        LR[(Local Registry\n(Harbor/Registry))]
        RR[(Remote Registry\n(Docker Hub/ECR))]
        OL[OverlayFS]
    end

    %% Component Interactions
    C -->|1. Pull Image| CR
    CR -->|2. Resolve| CS
    CS -->|3a. Check Local| LR
    CS -->|3b. Fallback Remote| RR

    %% Layer Processing
    CS -->|4. Store Metadata| MS
    CS -->|5. Prepare Snapshot| SS
    SS -->|6a. Mount Existing| OL
    SS -->|6b. Fetch Missing| LR

    %% OverlayFS Structure
    subgraph OL["OverlayFS Structure"]
        direction TB
        U[Upperdir\n(Writable)]
        W[Workdir\n(Temp)]
        L1[Layer 1\n(lowerdir)]
        L2[Layer 2\n(lowerdir)]
        L3[...]
    end

    %% Registry Fallback Path
    LR -->|Cache Miss| RR
    RR -->|Store Locally| LR

    %% Snapshotter Details
    SS -->|"7. Create\noverlay mounts"| OL
    OL -->|8. Merged View| Containerd