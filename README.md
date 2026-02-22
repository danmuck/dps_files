# **dps_files: A Decentralized Storage System**
*Combining High Performance, Strong Consistency, and Scalable Distribution*

## **Under Construction!**

---

## **Abstract**
dps_files introduces a novel approach to decentralized data storage and management by combining proven distributed systems techniques. It integrates:

1. **A Lightweight Operational Ledger** managed by a Raft root cluster for strong consistency and high performance.
2. **Kademlia-based Distributed Hash Table (DHT)** routing for efficient distribution and access to data replicas, ensuring high availability and scalability.

dps_files delivers a robust, fault-tolerant, and efficient solution for modern decentralized storage needs.

---

## **1. Introduction**

### **1.1 Background**
Decentralized storage systems have gained traction as an alternative to centralized cloud storage. Systems like **IPFS** and **Filecoin** demonstrate the power of distributed, content-addressed storage but lack strong operational consistency. Centralized systems like **Amazon DynamoDB** and **Ceph** provide performance and consistency but sacrifice decentralization.

dps_files bridges these gaps by blending:
- **Strong operational consistency** through Raft.
- **Decentralized availability and routing** through Kademlia DHT.

---

### **1.2 Key Challenges**

1. **Availability vs. Consistency**: Decentralized DHT systems lack centralized consistency guarantees.
2. **Fault Tolerance**: Ensuring reliable data replication while maintaining real-time operational efficiency is complex.

---

### **1.3 Objectives of dps_files**

dps_files aims to:

1. Provide a **highly available** and **scalable** decentralized storage solution using Kademlia DHT.
2. Maintain **strong consistency** for metadata and operational data through a Raft-managed ledger.
3. Minimize client overhead and computational costs for day-to-day operations.

---

## **2. System Architecture**

### **2.1 Components**

#### **2.1.1 Raft Root Cluster**
The Raft-managed root node cluster serves as the **centralized control plane** for:

- Managing metadata and user data in the **operational ledger**.
- Ensuring strong consistency for updates to metadata and routing information.

This component provides a **single source of truth** for all operational metadata, ensuring consistency without compromising scalability.

---

#### **2.1.2 Kademlia DHT**
Kademlia provides decentralized routing and replication for high availability.

- Clients store and retrieve data using **O(log n)** lookups for efficient performance.
- Distributed replicas are maintained to ensure fault tolerance and quick access.

Kademlia ensures that even if parts of the network fail, data remains accessible through multiple redundant nodes.

---

### **2.2 Workflow**

#### **2.2.1 Real-Time Operations**

1. Metadata and user data updates are written to the **operational ledger** in the Raft root cluster.
2. Kademlia routes client requests for data storage, retrieval, and replication.

This ensures fast and consistent real-time operations with minimal overhead on clients.

---

#### **2.2.2 Data Recovery**

1. Clients query Kademlia for replicas of data.
2. Data integrity is verified using cryptographic proofs (e.g., SHA-256 hash verification).

---

### **2.3 Security Model**

- **Encryption**:
  - **Symmetric Encryption** (AES-GCM) secures data at rest and in transit.
  - **Asymmetric Encryption** (ECDH) is used for secure key exchanges.
- **Integrity**:
  - SHA-256 hash verification ensures data integrity at both chunk and file level.
- **Access Control**:
  - Clients manage keys locally, ensuring that only authorized users can access their data.

---

## **3. Key Advantages**

### **3.1 Decoupled Performance and Security**

- Real-time operations use a lightweight operational ledger for speed and consistency.

This design ensures both performance and reliability.

---

### **3.2 Scalability and Availability**

- **Kademlia** provides efficient routing and distributed replication for high availability.
- The system scales horizontally with the addition of more DHT nodes.

---

### **3.3 Fault Tolerance**

- **Real-time fault tolerance** via Kademlia replicas.
- **Consensus-backed metadata** through Raft log replication.

This ensures the system remains operational and recoverable under various failure conditions.

---

### **3.4 Lightweight Client Overhead**

- Clients interact with Kademlia for fast lookups and replication.

This approach minimizes resource demands on clients.

---

## **4. Use Cases**

1. **Decentralized Backup Solutions**: Enterprises can maintain verifiable, immutable backups while distributing replicas for availability.
2. **Edge and IoT Environments**: Lightweight clients benefit from efficient Kademlia routing while offloading heavy storage to the root cluster.
3. **Versioned Data Storage**: dps_files enables secure versioning and auditability of critical data.

---

## **5. Conclusion**

dps_files combines the strengths of centralized consistency via Raft with decentralized availability via Kademlia DHT, achieving an optimal balance between performance, fault tolerance, and data integrity.

This architecture positions dps_files as a novel solution for decentralized storage and verifiable backups, addressing the limitations of current offerings while providing an extensible foundation for future innovations.

---

## **6. Future Work**

1. **Optimizing Backup Intervals**: Dynamically adjust snapshot generation based on workload patterns.
2. **Zero-Knowledge Proofs**: Implement ZKPs for secure, privacy-preserving data validation.
3. **Erasure Coding**: Explore more efficient replica storage mechanisms to reduce redundancy overhead.
4. **MultiNode Kademlia**: Modify Kademlia to take a value __n__ that signifies how many server nodes must exist in a client nodes routing table at any given time.
---
