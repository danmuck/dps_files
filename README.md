# **dps_files: A Dual-Ledger Decentralized Storage and Backup System**
*Combining High Performance, Strong Consistency, and Immutable Secure Backups*

---

## **Abstract**
dps_files introduces a novel approach to decentralized data storage and management by combining a dual-ledger architecture with proven distributed systems techniques. It integrates:

1. **A Lightweight Operational Ledger** managed by a Raft root cluster for strong consistency and high performance.
2. **A Blockchain Ledger** for immutable, secure, and auditable backups of encrypted data snapshots.
3. **Kademlia-based Distributed Hash Table (DHT)** routing for efficient distribution and access to data replicas, ensuring high availability and scalability.

By decoupling day-to-day operations from expensive blockchain overhead while retaining its security features for backups, dps_files delivers a robust, fault-tolerant, and efficient solution for modern decentralized storage needs.

---

## **1. Introduction**

### **1.1 Background**
Decentralized storage systems have gained traction as an alternative to centralized cloud storage. Systems like **IPFS** and **Filecoin** demonstrate the power of distributed, content-addressed storage but suffer from blockchain overhead or lack strong operational consistency. Centralized systems like **Amazon DynamoDB** and **Ceph** provide performance and consistency but sacrifice decentralization.

dps_files bridges these gaps by blending:
- **Strong operational consistency** through Raft.
- **Decentralized availability and routing** through Kademlia DHT.
- **Immutable, auditable backups** through a selective blockchain ledger.

---

### **1.2 Key Challenges**

1. **Performance vs. Security**: Blockchain-based solutions introduce significant computational and storage overhead.
2. **Availability vs. Consistency**: Decentralized DHT systems lack centralized consistency guarantees.
3. **Fault Tolerance**: Ensuring secure, immutable data backups while maintaining real-time operational efficiency is complex.

---

### **1.3 Objectives of dps_files**

dps_files aims to:

1. Provide a **highly available** and **scalable** decentralized storage solution using Kademlia DHT.
2. Maintain **strong consistency** for metadata and operational data through a Raft-managed ledger.
3. Offer **immutable and verifiable backups** via a blockchain ledger for critical, encrypted snapshots.
4. Minimize client overhead and computational costs for day-to-day operations.

---

## **2. System Architecture**

### **2.1 Components**

#### **2.1.1 Raft Root Cluster**
The Raft-managed root node cluster serves as the **centralized control plane** for:

- Managing metadata and user data in the **operational ledger**.
- Ensuring strong consistency for updates to metadata and routing information.
- Coordinating periodic backups to the blockchain ledger.

This component provides a **single source of truth** for all operational metadata, ensuring consistency without compromising scalability.

---

#### **2.1.2 Kademlia DHT**
Kademlia provides decentralized routing and replication for high availability.

- Clients store and retrieve data using **O(log n)** lookups for efficient performance.
- Distributed replicas are maintained to ensure fault tolerance and quick access.

Kademlia ensures that even if parts of the network fail, data remains accessible through multiple redundant nodes.

---

#### **2.1.3 Blockchain Ledger**
The blockchain ledger provides:

- Immutable, verifiable backups of encrypted data snapshots.
- Cryptographic integrity via hash chaining and Merkle Trees.
- A tamper-proof audit log for disaster recovery.

This ledger is **used selectively** to avoid performance bottlenecks but ensures that backups remain secure and verifiable.

---

### **2.2 Workflow**

#### **2.2.1 Real-Time Operations**

1. Metadata and user data updates are written to the **operational ledger** in the Raft root cluster.
2. Kademlia routes client requests for data storage, retrieval, and replication.

This ensures fast and consistent real-time operations with minimal overhead on clients.

---

#### **2.2.2 Backup Generation**

1. At periodic intervals or after critical updates, encrypted snapshots of important data are generated.
2. The Raft root cluster pushes these snapshots to the **blockchain ledger** for immutable storage.

This process creates an auditable history of critical data, ensuring disaster recovery capabilities.

---

#### **2.2.3 Data Recovery**

1. Clients query Kademlia for replicas of data.
2. If replicas are unavailable, the blockchain ledger is used to retrieve the latest backup.
3. Data integrity is verified using cryptographic proofs (e.g., Merkle root hashes).

---

### **2.3 Security Model**

- **Encryption**:
  - **Symmetric Encryption** (AES-GCM) secures data at rest and in transit.
  - **Asymmetric Encryption** (ECDH) is used for secure key exchanges.
- **Integrity**:
  - The blockchain ledger guarantees immutability through hash chaining and Merkle proofs.
- **Access Control**:
  - Clients manage keys locally, ensuring that only authorized users can access their data.

---

## **3. Key Advantages**

### **3.1 Decoupled Performance and Security**

- Real-time operations use a lightweight operational ledger for speed.
- Backups are stored securely on the blockchain ledger without burdening regular operations.

This separation ensures both performance and long-term security.

---

### **3.2 Scalability and Availability**

- **Kademlia** provides efficient routing and distributed replication for high availability.
- The system scales horizontally with the addition of more DHT nodes.

---

### **3.3 Multi-Layer Fault Tolerance**

- **Real-time fault tolerance** via Kademlia replicas.
- **Long-term fault tolerance** through secure blockchain-backed backups.

This ensures the system remains operational and recoverable under various failure conditions.

---

### **3.4 Lightweight Client Overhead**

- Clients interact with Kademlia for fast lookups and replication.
- Blockchain interaction is **optional** and reserved for critical backup operations.

This approach minimizes resource demands on clients.

---

## **4. Use Cases**

1. **Decentralized Backup Solutions**: Enterprises can maintain verifiable, immutable backups while distributing replicas for availability.
2. **Edge and IoT Environments**: Lightweight clients benefit from efficient Kademlia routing while offloading heavy storage to the root cluster.
3. **Versioned Data Storage**: dps_files enables secure versioning and auditability of critical data.

---

## **5. Conclusion**

dps_files introduces a dual-ledger system that combines the strengths of centralized consistency, decentralized availability, and blockchain security. By decoupling real-time operations from secure backups, it achieves an optimal balance between performance, fault tolerance, and data integrity.

This architecture positions dps_files as a novel solution for decentralized storage and verifiable backups, addressing the limitations of current offerings while providing an extensible foundation for future innovations.

---

## **6. Future Work**

1. **Optimizing Backup Intervals**: Dynamically adjust snapshot generation based on workload patterns.
2. **Zero-Knowledge Proofs**: Implement ZKPs for secure, privacy-preserving data validation.
3. **Erasure Coding**: Explore more efficient replica storage mechanisms to reduce redundancy overhead.
4. **MultiNode Kademlia**: Modify Kademlia to take a value __n__ that signifies how many server nodes must exist in a client nodes routing table at any given time.
---
