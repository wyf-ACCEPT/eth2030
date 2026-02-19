package das

import (
	"errors"
	"math"
	"sync"
)

// Errors for sample optimization.
var (
	ErrInvalidSecurityParam = errors.New("das/sample: security parameter must be positive")
	ErrInvalidBlobCount     = errors.New("das/sample: blob count must be positive")
	ErrInvalidNetworkHealth = errors.New("das/sample: network health must be in [0, 1]")
	ErrInvalidFailureRate   = errors.New("das/sample: failure rate must be in [0, 1]")
	ErrInvalidSampleSize    = errors.New("das/sample: sample size must be positive")
)

// SampleOptimizerConfig configures the sample size optimizer.
type SampleOptimizerConfig struct {
	// MinSamples is the minimum number of samples per blob.
	MinSamples int

	// MaxSamples is the maximum number of samples per blob.
	MaxSamples int

	// TargetConfidence is the desired confidence level in [0, 1].
	TargetConfidence float64

	// SecurityMargin is an additive margin applied to the computed sample count.
	SecurityMargin int
}

// DefaultSampleOptimizerConfig returns reasonable defaults for PeerDAS.
func DefaultSampleOptimizerConfig() SampleOptimizerConfig {
	return SampleOptimizerConfig{
		MinSamples:       SamplesPerSlot,       // 8
		MaxSamples:       int(NumberOfColumns),  // 128
		TargetConfidence: 0.999,
		SecurityMargin:   2,
	}
}

// SamplingPlan describes a concrete sampling strategy for a slot.
type SamplingPlan struct {
	// SamplesPerBlob is the number of samples to request per blob.
	SamplesPerBlob int

	// TotalSamples is the total samples across all blobs.
	TotalSamples int

	// SecurityLevel is the achieved security parameter (bits).
	SecurityLevel int

	// ConfidenceLevel is the probability that data is available given
	// the sampling succeeded, in [0, 1].
	ConfidenceLevel float64
}

// SamplingVerdict is the result of validating a sampling outcome.
type SamplingVerdict struct {
	// Sufficient is true when enough samples were received.
	Sufficient bool

	// Confidence is the estimated confidence given the received samples.
	Confidence float64

	// MissingSamples is how many more samples would be needed for the
	// plan to be considered fulfilled. Zero when Sufficient is true.
	MissingSamples int
}

// SampleOptimizer dynamically optimizes DAS sampling parameters based on
// network conditions and security requirements. It implements the
// "decreased sample size" optimization from the Data Layer roadmap.
type SampleOptimizer struct {
	mu     sync.RWMutex
	config SampleOptimizerConfig
}

// NewSampleOptimizer creates a new optimizer with the given config.
// Defaults are applied for zero-value fields.
func NewSampleOptimizer(config SampleOptimizerConfig) *SampleOptimizer {
	if config.MinSamples <= 0 {
		config.MinSamples = SamplesPerSlot
	}
	if config.MaxSamples <= 0 {
		config.MaxSamples = int(NumberOfColumns)
	}
	if config.MaxSamples < config.MinSamples {
		config.MaxSamples = config.MinSamples
	}
	if config.TargetConfidence <= 0 || config.TargetConfidence > 1 {
		config.TargetConfidence = 0.999
	}
	if config.SecurityMargin < 0 {
		config.SecurityMargin = 0
	}
	return &SampleOptimizer{config: config}
}

// CalculateOptimalSamples returns the minimum number of samples needed
// per blob to achieve the requested security parameter (in bits).
//
// The model assumes an adversary withholds some fraction of cells. With
// k samples drawn uniformly from N columns, the probability that at
// least one withheld cell is sampled is 1 - ((N-1)/N)^k. We want that
// probability >= 1 - 2^{-securityParam}.
//
// Solving: k >= securityParam * ln(2) / ln(N/(N-1)).
func (so *SampleOptimizer) CalculateOptimalSamples(blobCount int, securityParam int) int {
	if blobCount <= 0 || securityParam <= 0 {
		return so.clamp(so.config.MinSamples)
	}

	n := float64(NumberOfColumns)
	// k >= securityParam * ln(2) / ln(N / (N-1))
	ratio := n / (n - 1)
	k := float64(securityParam) * math.Ln2 / math.Log(ratio)

	samples := int(math.Ceil(k)) + so.config.SecurityMargin
	return so.clamp(samples)
}

// AdaptiveSampling produces a SamplingPlan adjusted to network conditions.
// networkHealth is in [0, 1] where 1.0 means a perfectly healthy network
// and lower values cause more conservative (larger) sample counts.
func (so *SampleOptimizer) AdaptiveSampling(blobCount int, networkHealth float64) *SamplingPlan {
	so.mu.RLock()
	cfg := so.config
	so.mu.RUnlock()

	if blobCount <= 0 {
		blobCount = 1
	}
	if networkHealth < 0 {
		networkHealth = 0
	}
	if networkHealth > 1 {
		networkHealth = 1
	}

	// Base samples derived from the target confidence.
	// We use the target confidence to derive an effective security param.
	// confidence = 1 - 2^{-securityBits}  =>  securityBits = -log2(1 - confidence)
	secBits := -math.Log2(1 - cfg.TargetConfidence)
	baseSamples := so.CalculateOptimalSamples(blobCount, int(math.Ceil(secBits)))

	// When network health degrades, scale up samples. At health=1 factor=1,
	// at health=0 factor=2 (double the samples).
	factor := 2.0 - networkHealth
	adjusted := int(math.Ceil(float64(baseSamples) * factor))
	samplesPerBlob := so.clamp(adjusted)

	totalSamples := samplesPerBlob * blobCount

	// Compute achieved confidence: 1 - ((N-1)/N)^k
	n := float64(NumberOfColumns)
	confidence := 1.0 - math.Pow((n-1)/n, float64(samplesPerBlob))

	// Security level in bits: -log2(1 - confidence)
	secLevel := 0
	if confidence < 1 {
		secLevel = int(math.Floor(-math.Log2(1 - confidence)))
	} else {
		secLevel = 128 // cap at 128 bits
	}

	return &SamplingPlan{
		SamplesPerBlob:  samplesPerBlob,
		TotalSamples:    totalSamples,
		SecurityLevel:   secLevel,
		ConfidenceLevel: confidence,
	}
}

// ValidateSamplingResult checks whether a completed sampling round met the
// plan's requirements.
func (so *SampleOptimizer) ValidateSamplingResult(plan *SamplingPlan, receivedSamples int) *SamplingVerdict {
	if plan == nil {
		return &SamplingVerdict{Sufficient: false, Confidence: 0, MissingSamples: 0}
	}

	needed := plan.TotalSamples
	if receivedSamples < 0 {
		receivedSamples = 0
	}

	missing := needed - receivedSamples
	if missing < 0 {
		missing = 0
	}

	// Achieved confidence based on actual received samples.
	n := float64(NumberOfColumns)
	confidence := 1.0 - math.Pow((n-1)/n, float64(receivedSamples))

	return &SamplingVerdict{
		Sufficient:     receivedSamples >= needed,
		Confidence:     confidence,
		MissingSamples: missing,
	}
}

// AdjustSampleSize dynamically adjusts the sample size based on observed
// failure rate. A higher failure rate triggers more samples; a low failure
// rate allows decreasing towards the minimum.
//
// failureRate is in [0, 1]. Returns the adjusted sample size.
func (so *SampleOptimizer) AdjustSampleSize(currentSize int, failureRate float64) int {
	if currentSize <= 0 {
		currentSize = so.config.MinSamples
	}
	if failureRate < 0 {
		failureRate = 0
	}
	if failureRate > 1 {
		failureRate = 1
	}

	// Scale: at failureRate=0 we shrink towards min; at failureRate=1 we
	// grow towards max. Linear interpolation with hysteresis band.
	switch {
	case failureRate > 0.2:
		// Increase: scale by (1 + failureRate).
		increased := int(math.Ceil(float64(currentSize) * (1 + failureRate)))
		return so.clamp(increased)
	case failureRate < 0.05:
		// Decrease: reduce by 10%.
		decreased := int(math.Floor(float64(currentSize) * 0.9))
		return so.clamp(decreased)
	default:
		// Within acceptable band, keep current.
		return so.clamp(currentSize)
	}
}

// EstimateNetworkLoad estimates the network bandwidth in bytes consumed by
// DAS sampling with the given parameters. Each sample is one cell (2048 bytes)
// plus a 48-byte KZG proof.
func (so *SampleOptimizer) EstimateNetworkLoad(blobCount, sampleSize int) uint64 {
	if blobCount <= 0 || sampleSize <= 0 {
		return 0
	}
	bytesPerSample := uint64(BytesPerCell + 48) // cell + proof
	return uint64(blobCount) * uint64(sampleSize) * bytesPerSample
}

// clamp restricts v to [MinSamples, MaxSamples].
func (so *SampleOptimizer) clamp(v int) int {
	if v < so.config.MinSamples {
		return so.config.MinSamples
	}
	if v > so.config.MaxSamples {
		return so.config.MaxSamples
	}
	return v
}
