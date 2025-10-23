```mermaid
graph TD;
    classDef default font-size:20px,font-family:Arial;
    classDef cluster fill:#fafafa,stroke:#bbb,stroke-width:2px,color:#326ce5;
    classDef registry fill:#e0f7fa,stroke:#00008b,stroke-width:2px,color:#326ce5;
    classDef outer fill:white,stroke:#00008b,stroke-width:2px,color:#a9a9a9;
    classDef layer fill:#ffffff,stroke:#adb5bd,stroke-width:1.5px;

    subgraph Cluster["P2P Image Distribution"]
        direction LR
        style Cluster font-size:16px

        subgraph Node1["Node A"]
            style Node1 font-size:16px
            kubelet["kubectl run pytorch-ascend:v1"]
            subgraph ctr1["Containerd"]
                subgraph store1["Content Store"]
                    l2["sha256:l2"]:::layer
                end
            end
            kubelet --> ctr1
        end

        subgraph Node2["Node B"]
            style Node2 font-size:16px
            subgraph ctr2["Containerd"]
                subgraph store2["Content Store"]
                    l6["sha256:l6"]:::layer
                    l3["sha256:l3"]:::layer
                end
            end
        end
    end

    subgraph Registry["Upstream Registry"]
        style Registry font-size:16px
        acr["cmc.huawei.com"]:::registry
    end

    %% Peer connections
    Node1 -.->|"P2P Pull: l6"| l6
    Node1 -.->|"P2P Pull: l3"| l3

    %% Registry connection
    Node1 -->|"Registry Pull: l1"| acr

    %% Style applications
    class Node1,Node2 cluster;
    class Cluster outer;