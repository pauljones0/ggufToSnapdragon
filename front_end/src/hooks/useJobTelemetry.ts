import { useState, useEffect } from "react";
import { doc, onSnapshot, collection, query, where } from "firebase/firestore";
import { db } from "@/lib/firebase";

export type JobStatus = "idle" | "loading" | "queued" | "provisioning" | "downloading" | "compiling" | "uploading" | "completed" | "failed";

export function useJobTelemetry(jobId: string, initialStatus: JobStatus, jobCreatedAt: Date | null) {
    const [status, setStatus] = useState<JobStatus>(initialStatus);
    const [errorMessage, setErrorMessage] = useState("");
    const [queuePosition, setQueuePosition] = useState<number | null>(null);

    useEffect(() => {
        if (!jobId) {
            setStatus("idle");
            setErrorMessage("");
            setQueuePosition(null);
            return;
        }

        const jobRef = doc(db, "Jobs", jobId);
        const unsubscribeJob = onSnapshot(jobRef, (docSnap) => {
            if (docSnap.exists()) {
                const data = docSnap.data();
                setStatus(data.status.toLowerCase() as JobStatus);

                if (data.status === "Failed" && data.error_message) {
                    setErrorMessage(data.error_message);
                }
            }
        });

        let unsubscribeQueue: (() => void) | null = null;
        if ((status === "queued" || status === "provisioning") && jobCreatedAt) {
            const jobsRef = collection(db, "Jobs");
            const q = query(
                jobsRef,
                where("status", "in", ["Queued", "Provisioning"]),
                where("created_at", "<", jobCreatedAt)
            );
            unsubscribeQueue = onSnapshot(q, (snapshot) => {
                setQueuePosition(snapshot.docs.length + 1);
            }, (err) => {
                console.error("Queue stream failed", err);
            });
        }

        return () => {
            unsubscribeJob();
            if (unsubscribeQueue) unsubscribeQueue();
        };
    }, [jobId, status, jobCreatedAt]);

    return { status, setStatus, errorMessage, setErrorMessage, queuePosition, setQueuePosition };
}
