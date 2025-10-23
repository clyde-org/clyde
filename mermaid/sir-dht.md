```mermaid
graph TD;
    classDef default font-size:18px,font-family:Arial;
    
    subgraph Cluster["DHT Topology with Peer Discovery"]
        style Cluster font-size:20px,stroke-width:3px
        
        %% Existing Nodes (Top Row)
        subgraph Node1["Node A"]
            style Node1 font-size:15px
            subgraph shadow-1["Clyde"]
                dht-1("DHT"):::dhtstyle
                leader-1("Leader"):::leader
            end
        end
        
        subgraph Node2["Node B"]
            style Node2 font-size:15px
            subgraph shadow-2["Clyde"]
                dht-2("DHT"):::dhtstyle
                follower-2("Follower"):::follower
            end
        end

        subgraph Node3["Node C"]
            style Node3 font-size:15px
            subgraph shadow-3["Clyde"]
                dht-3("DHT"):::dhtstyle
                follower-3("Follower"):::follower
            end
        end

        %% New Node (Bottom Center)
        subgraph Node4["New Node"]
            style Node4 font-size:15px
            subgraph shadow-4["Clyde"]
                dht-4("DHT"):::dhtstyle
                candidate-4("Candidate"):::candidate
            end
        end

        %% %% Horizontal Layout for Top Nodes
        %% Node1 --- Node2
        %% Node2 --- Node3
        %% Node3 --- Node1

        %% Vertical Connection to New Node
        %% Node2 --- Node4

        %% P2P Connections
        dht-1 <==> |"P2P Network"| dht-2
        dht-1 <==> |"P2P Network"| dht-3
        dht-2 <==> |"P2P Network"| dht-3

        %% Discovery Processes
        dht-4 -.-> |"1 Seed Discovery"| dht-1
        dht-4 -.-> |"2 Join Request"| dht-1
        dht-1 --> |"3 Topology Sync"| dht-4
        dht-4 ==> |"4 Leader Election"| dht-1
        leader-1 --> |"Heartbeat"| follower-2
        leader-1 --> |"Heartbeat"| follower-3
    end

    %% CSS Styles
    classDef cluster fill:#fafafa,stroke:#bbb,stroke-width:3px;
    classDef node fill:#e6f3ff,stroke:#326ce5,stroke-width:2px;
    classDef dhtstyle fill:#ffffff,stroke:#666666,stroke-width:2px,font-size:18px;
    classDef leader fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px,font-size:18px;
    classDef follower fill:#fff3e0,stroke:#fb8c00,stroke-width:2px,font-size:18px;
    classDef candidate fill:#fce4ec,stroke:#c2185b,stroke-width:2px,font-size:18px;
    classDef newnode fill:#f3e5f5,stroke:#7b1fa2,stroke-width:2px,dashed,font-size:18px;

    %% Apply Styles
    class Node1,Node2,Node3,Node4 node;
    class leader-1 leader;
    class follower-2,follower-3 follower;
    class candidate-4 candidate;
    class Node4 newnode;
    class dht-1,dht-2,dht-3,dht-4 dhtstyle;