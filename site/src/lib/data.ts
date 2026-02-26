export interface Stat {
  label: string;
  value: number;
  suffix?: string;
  prefix?: string;
}

export interface Feature {
  title: string;
  description: string;
  icon: string;
  color: 'purple' | 'blue' | 'teal' | 'pink';
  tags: string[];
}

export interface RoadmapPhase {
  name: string;
  year: string;
  coverage: string;
  highlights: string[];
}

export interface ArchitectureLayer {
  name: string;
  color: 'purple' | 'teal' | 'blue';
  tracks: { name: string; items: string[] }[];
}

export const stats: Stat[] = [
  { label: 'Packages', value: 50 },
  { label: 'Tests', value: 18000, suffix: '+' },
  { label: 'EIPs Implemented', value: 58 },
  { label: 'LOC', value: 702, suffix: 'K' },
  { label: 'EF Conformance', value: 100, suffix: '%' },
  { label: 'Roadmap Items', value: 65, suffix: '/65' },
];

export const features: Feature[] = [
  {
    title: 'Full EVM Execution',
    description: '164+ opcodes, 24 precompiles, EOF container support. go-ethereum v1.17.0 backend with 13 custom precompiles.',
    icon: '\u2699\uFE0F',
    color: 'purple',
    tags: ['EIP-3540', 'EOF', '164+ opcodes'],
  },
  {
    title: 'Post-Quantum Cryptography',
    description: 'ML-DSA-65 (FIPS 204), Dilithium3, Falcon512, SPHINCS+ with hybrid signing and PQ algorithm registry.',
    icon: '\uD83D\uDD12',
    color: 'teal',
    tags: ['ML-DSA-65', 'SPHINCS+', 'Falcon512'],
  },
  {
    title: 'Native Rollups',
    description: 'EIP-8079 EXECUTE precompile and anchor contract for L1-native rollup execution.',
    icon: '\uD83C\uDF00',
    color: 'blue',
    tags: ['EIP-8079', 'EXECUTE', 'Anchor'],
  },
  {
    title: 'zkVM Framework',
    description: 'Canonical RISC-V RV32IM guest, STF executor for state transition proofs, zkISA bridge with host ABI.',
    icon: '\uD83E\uDDE0',
    color: 'pink',
    tags: ['RISC-V', 'STF', 'zkISA'],
  },
  {
    title: '3-Slot Finality',
    description: '3SF with quick slots (6s), 4-slot epochs, 1-epoch finality, 1M attestations/slot via parallel BLS.',
    icon: '\u26A1',
    color: 'purple',
    tags: ['3SF', '6s slots', '1M attestations'],
  },
  {
    title: 'PeerDAS',
    description: 'Data availability sampling, custody proofs, blob streaming, variable-size blobs, Reed-Solomon reconstruction.',
    icon: '\uD83D\uDCE1',
    color: 'teal',
    tags: ['EIP-7594', 'DAS', 'Blob streaming'],
  },
  {
    title: 'ePBS + FOCIL',
    description: 'Enshrined PBS (EIP-7732) with distributed builder and fork-choice enforced inclusion lists (EIP-7805).',
    icon: '\uD83C\uDFD7\uFE0F',
    color: 'blue',
    tags: ['EIP-7732', 'EIP-7805', 'MEV'],
  },
  {
    title: 'Proof Aggregation',
    description: 'SNARK/STARK/IPA/KZG registry, mandatory 3-of-5 proof system with prover assignment and penalties.',
    icon: '\uD83D\uDD17',
    color: 'pink',
    tags: ['Groth16', 'PLONK', 'KZG'],
  },
  {
    title: 'Encrypted Mempool',
    description: 'Commit-reveal scheme with Shamir/Feldman threshold crypto and ElGamal encryption for MEV protection.',
    icon: '\uD83D\uDEE1\uFE0F',
    color: 'purple',
    tags: ['Threshold', 'ElGamal', 'MEV'],
  },
  {
    title: '100% EF State Tests',
    description: '36,126/36,126 Ethereum Foundation state tests passing via go-ethereum v1.17.0 backend integration.',
    icon: '\u2705',
    color: 'teal',
    tags: ['36,126 tests', '57 categories', '100%'],
  },
  {
    title: 'Complete Networking',
    description: 'devp2p, Discovery V5, gossip pub/sub, Portal network, snap sync, beam sync, DNS discovery.',
    icon: '\uD83C\uDF10',
    color: 'blue',
    tags: ['devp2p', 'Portal', 'Snap sync'],
  },
  {
    title: 'Engine API V3-V7',
    description: 'Full payload lifecycle, forkchoice, 50+ JSON-RPC methods, WebSocket subscriptions, Beacon API.',
    icon: '\uD83D\uDD0C',
    color: 'pink',
    tags: ['50+ RPC', 'WebSocket', 'Beacon API'],
  },
];

export const roadmapPhases: RoadmapPhase[] = [
  {
    name: 'Glamsterdam',
    year: '2026',
    coverage: '~99%',
    highlights: ['ePBS', 'FOCIL', 'BALs', 'Native AA', '18 EIP repricing', 'Sparse blobpool'],
  },
  {
    name: 'Hegotá',
    year: '2026-27',
    coverage: '~97%',
    highlights: ['Multidim gas', 'Payload chunking', 'NTT precompile', 'Encrypted mempool', 'SSZ transactions'],
  },
  {
    name: 'I+',
    year: '2027',
    coverage: '~97%',
    highlights: ['Native rollups', 'zkVM framework', 'VOPS', 'PQ crypto', 'Beam sync'],
  },
  {
    name: 'J+',
    year: '2027-28',
    coverage: '~95%',
    highlights: ['Light client', 'Verkle migration', 'Variable blobs', 'Reed-Solomon pipeline'],
  },
  {
    name: 'K+',
    year: '2028',
    coverage: '~97%',
    highlights: ['3SF', '6-sec slots', 'Mandatory 3-of-5 proofs', '1M attestations', 'CL proof circuits'],
  },
  {
    name: 'L+',
    year: '2029',
    coverage: '~97%',
    highlights: ['Endgame finality', 'PQ attestations', 'APS', 'Custody proofs', 'jeanVM aggregation'],
  },
  {
    name: 'M+',
    year: '2029+',
    coverage: '~95%',
    highlights: ['Gigagas L1', 'Canonical zkVM', 'Gas futures', 'PQ blob commitments'],
  },
  {
    name: '2030++',
    year: 'Long term',
    coverage: '~95%',
    highlights: ['VDF randomness', 'Distributed builders', 'Shielded transfers', '51% auto-recovery'],
  },
];

export const architectureLayers: ArchitectureLayer[] = [
  {
    name: 'Consensus Layer',
    color: 'purple',
    tracks: [
      { name: 'Latency', items: ['Quick slots', '3SF', '1-epoch finality', '6s slots'] },
      { name: 'Accessibility', items: ['ePBS', 'FOCIL', 'APS', '1M attestations'] },
      { name: 'Cryptography', items: ['PQ attestations', 'CL proofs', 'VDF', 'Secret proposers'] },
    ],
  },
  {
    name: 'Data Layer',
    color: 'teal',
    tracks: [
      { name: 'Throughput', items: ['Sparse blobpool', 'Blob reconstruction', 'BPO schedules'] },
      { name: 'Types', items: ['Blob streaming', 'Futures', 'Variable-size', 'Custody proofs'] },
    ],
  },
  {
    name: 'Execution Layer',
    color: 'blue',
    tracks: [
      { name: 'Throughput', items: ['Gas repricing', 'Multidim gas', 'Payload chunking', 'Gigagas'] },
      { name: 'EVM', items: ['Native AA', 'NTT precompile', 'Native rollups', 'zkISA'] },
      { name: 'Sustainability', items: ['BALs', 'Binary tree', 'VOPS', 'Endgame state'] },
      { name: 'Cryptography', items: ['Encrypted mempool', 'PQ transactions', 'Shielded transfers'] },
    ],
  },
];

export const gettingStartedCommands = [
  { comment: '# Clone the repository', command: 'git clone https://github.com/jiayaoqijia/eth2030.git' },
  { comment: '# Build all 50 packages', command: 'cd eth2030/pkg && go build ./...' },
  { comment: '# Run 18,000+ tests', command: 'go test ./...' },
  { comment: '# Build the geth-embedded node', command: 'go build -o eth2030-geth ./cmd/eth2030-geth/' },
  { comment: '# Sync with Sepolia testnet', command: './eth2030-geth --network sepolia --datadir ~/.eth2030-sepolia' },
];
