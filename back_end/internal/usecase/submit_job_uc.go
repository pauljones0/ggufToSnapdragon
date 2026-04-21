package usecase

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"hexforge-backend/internal/domain"
	"hexforge-backend/models" // Keeping the existing hardware models package for now
)

type SubmitJobUseCase struct {
	JobRepo   domain.JobRepository
	EventBus  domain.EventPublisher
	TaskQueue domain.CloudTasksClient
	// Added logger as a struct field could be used here
}

func NewSubmitJobUseCase(repo domain.JobRepository, bus domain.EventPublisher, taskQueue domain.CloudTasksClient) *SubmitJobUseCase {
	return &SubmitJobUseCase{
		JobRepo:   repo,
		EventBus:  bus,
		TaskQueue: taskQueue,
	}
}

// Execute performs the business logic to validate and enqueue a job.
func (uc *SubmitJobUseCase) Execute(ctx context.Context, req domain.JobRequest, traceID, spanID string) (*domain.JobRecord, error) {
	// Replicating business logic from the monolithic SubmitJob

	// 1. Strict URL sanitization
	// The character set deliberately uses actual control characters (\n \r \t via Go escape sequences),
	// NOT the two-character sequences \\n \\r \\t (which would match the letters n, r, t).
	if strings.ContainsAny(req.HFUrl, ";&|`$<>\\'\"\n\r\t") {
		return nil, fmt.Errorf("invalid characters in URL")
	}

	maliciousRepos := []string{"unfiltered-malware-models", "poc-malicious-gguf"}
	for _, blacklisted := range maliciousRepos {
		if strings.Contains(strings.ToLower(req.HFUrl), strings.ToLower(blacklisted)) {
			return nil, fmt.Errorf("repository is blacklisted")
		}
	}

	// 2. Size extraction
	paramsSizeRe := regexp.MustCompile(`(?i)([\d\.]+)b`)
	matches := paramsSizeRe.FindStringSubmatch(req.HFUrl)
	if len(matches) < 2 {
		return nil, fmt.Errorf("incompatible model architecture")
	}

	sizeFloat, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid model size format")
	}
	paramSizeBillions := sizeFloat

	// 3. Hardware verification
	deviceDict := models.GetDeviceDictionary()
	deviceConfig, exists := deviceDict[req.PhoneModel]
	if !exists {
		return nil, fmt.Errorf("unrecognized snapdragon component")
	}

	profiles := models.GetHardwareProfiles()
	profile, ok := profiles[deviceConfig.HexagonDSPArchitecture]
	if !ok {
		return nil, fmt.Errorf("unrecognized DSP arch mapped: %s", deviceConfig.HexagonDSPArchitecture)
	}

	if paramSizeBillions > float64(profile.MaxNPUOnlyModelSizeB) {
		return nil, fmt.Errorf("model size strictly exceeds %dB limit for %s", profile.MaxNPUOnlyModelSizeB, deviceConfig.HexagonDSPArchitecture)
	}

	targetVCpu := profile.RecommendedVCpuCount
	if targetVCpu == 0 {
		targetVCpu = domain.StandardVCpuCount
	}

	// 4. File Size HEAD Check omitted in usecase to keep it fast, or could be passed via HFValidator port.

	// 5. Global queue check
	queuedCount, err := uc.JobRepo.GetGlobalQueuedCount(ctx, 50)
	if err != nil {
		return nil, err
	}
	if queuedCount >= 50 {
		return nil, fmt.Errorf("queue full")
	}

	activeCount, err := uc.JobRepo.GetActiveJobCountForUser(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if activeCount >= 1 {
		return nil, fmt.Errorf("already active job in pipeline")
	}

	// 6. Cache check
	if cached, err := uc.JobRepo.FindCompletedCache(ctx, req.HFUrl, deviceConfig.HexagonDSPArchitecture); err == nil && cached != nil {
		// Cache hit! Map to a new job instantly.
		jobID := uuid.New().String()
		rec := &domain.JobRecord{
			JobID:                 jobID,
			UserID:                req.UserID,
			HFUrl:                 req.HFUrl,
			ParameterSizeBillions: int(paramSizeBillions),
			PhoneModel:            req.PhoneModel,
			Chipset:               deviceConfig.Chipset,
			DSPArch:               deviceConfig.HexagonDSPArchitecture,
			TargetRAM:             profile.RecommendedHostRAMGB,
			TargetSwap:            profile.RecommendedHostSwapGB,
			TargetVCpu:            targetVCpu,
			NativeINT4Support:     profile.NativeINT4Support,
			HasHMX:                profile.HasHMX,
			NeedsLogitsOffload:    profile.NeedsLogitsOffload,
			MaxSessionMemoryGB:    profile.MaxSessionMemoryGB,
			VTCMSizeKB:            profile.VTCMSizeKB,
			HTPGeneration:         profile.HTPGeneration,
			MmapBudgetMB:          profile.MmapBudgetMB,
			MoECapable:            profile.MoECapable,
			KVOffsetBufferMB:      profile.KVOffsetBufferMB,
			EstimatedINT8TOPS:     profile.EstimatedINT8TOPS,
			MemBandwidthGBps:      profile.MemBandwidthGBps,
			Status:                "Completed",
			TraceID:               traceID,
			CreatedAt:             time.Now().UTC(),
			UpdatedAt:             time.Now().UTC(),
		}
		_ = uc.JobRepo.SaveJob(ctx, rec)
		return rec, nil
	}

	// 7. Create normal job
	jobID := uuid.New().String()
	rec := &domain.JobRecord{
		JobID:                 jobID,
		UserID:                req.UserID,
		HFUrl:                 req.HFUrl,
		ParameterSizeBillions: int(paramSizeBillions),
		PhoneModel:            req.PhoneModel,
		Chipset:               deviceConfig.Chipset,
		DSPArch:               deviceConfig.HexagonDSPArchitecture,
		TargetRAM:             profile.RecommendedHostRAMGB,
		TargetSwap:            profile.RecommendedHostSwapGB,
		TargetVCpu:            targetVCpu,
		NativeINT4Support:     profile.NativeINT4Support,
		HasHMX:                profile.HasHMX,
		NeedsLogitsOffload:    profile.NeedsLogitsOffload,
		MaxSessionMemoryGB:    profile.MaxSessionMemoryGB,
		VTCMSizeKB:            profile.VTCMSizeKB,
		HTPGeneration:         profile.HTPGeneration,
		MmapBudgetMB:          profile.MmapBudgetMB,
		MoECapable:            profile.MoECapable,
		KVOffsetBufferMB:      profile.KVOffsetBufferMB,
		EstimatedINT8TOPS:     profile.EstimatedINT8TOPS,
		MemBandwidthGBps:      profile.MemBandwidthGBps,
		Status:                "Queued",
		TraceID:               traceID,
		CreatedAt:             time.Now().UTC(),
		UpdatedAt:             time.Now().UTC(),
	}

	if err := uc.JobRepo.SaveJob(ctx, rec); err != nil {
		return nil, fmt.Errorf("failed to save job to repo: %v", err)
	}

	// 8. Enqueue background task
	if err := uc.TaskQueue.EnqueueJob(ctx, rec.JobID, traceID); err != nil {
		// Enqueue failed — do NOT publish event; the job will be retried by Cloud Tasks.
	} else {
		uc.EventBus.PublishJobEvent(ctx, domain.EventJobSubmitted, rec.JobID, traceID, spanID, map[string]string{
			"user_id":     req.UserID,
			"phone_model": req.PhoneModel,
		})
	}

	return rec, nil
}
