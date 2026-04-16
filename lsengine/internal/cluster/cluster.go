// internal/cluster/cluster.go
package cluster

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type ClusterManager struct {
	workers    []*os.Process
	master     bool
	workerID   int
	mu         sync.RWMutex
	healthChan chan bool
	stopChan   chan struct{}
}

func NewClusterManager() *ClusterManager {
	return &ClusterManager{
		workers:    make([]*os.Process, 0),
		master:     true,
		healthChan: make(chan bool, 10),
		stopChan:   make(chan struct{}),
	}
}

func (cm *ClusterManager) StartWorkers(count int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for i := 0; i < count; i++ {
		cmd := exec.Command(os.Args[0], "--worker", strconv.Itoa(i))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		cm.workers = append(cm.workers, cmd.Process)
		log.Printf("[Cluster] Worker %d iniciado con PID %d", i, cmd.Process.Pid)
	}

	go cm.monitorWorkers()
	return nil
}

func (cm *ClusterManager) monitorWorkers() {
	ticker := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-ticker.C:
			cm.mu.RLock()
			for i, proc := range cm.workers {
				if proc == nil {
					continue
				}
				if err := proc.Signal(syscall.Signal(0)); err != nil {
					log.Printf("[Cluster] Worker %d no responde, reiniciando...", i)
					cm.restartWorker(i)
				}
			}
			cm.mu.RUnlock()
		case <-cm.stopChan:
			ticker.Stop()
			return
		}
	}
}

func (cm *ClusterManager) restartWorker(index int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if index >= len(cm.workers) {
		return
	}

	if cm.workers[index] != nil {
		cm.workers[index].Kill()
	}

	cmd := exec.Command(os.Args[0], "--worker", strconv.Itoa(index))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("[Cluster] Error reiniciando worker %d: %v", index, err)
		return
	}
	cm.workers[index] = cmd.Process
	log.Printf("[Cluster] Worker %d reiniciado con PID %d", index, cmd.Process.Pid)
}

func (cm *ClusterManager) Stop() {
	close(cm.stopChan)
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, proc := range cm.workers {
		if proc != nil {
			proc.Kill()
		}
	}
	cm.workers = make([]*os.Process, 0)
}