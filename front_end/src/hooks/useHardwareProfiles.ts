import { useState, useEffect } from "react";
import { collection, getDocs } from "firebase/firestore";
import { db } from "@/lib/firebase";

export interface HardwareProfile {
    phone_model: string;
    max_npu_billions: number;
}

export function useHardwareProfiles() {
    const [hardwareProfiles, setHardwareProfiles] = useState<HardwareProfile[]>([]);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        const fetchProfiles = async () => {
            try {
                const profilesRef = collection(db, "HardwareProfiles");
                const snapshot = await getDocs(profilesRef);
                const profiles: HardwareProfile[] = [];
                snapshot.forEach(doc => {
                    profiles.push({
                        phone_model: doc.data().phone_model,
                        max_npu_billions: doc.data().max_npu_billions
                    });
                });
                setHardwareProfiles(profiles);
            } catch (err) {
                console.error("Failed to load hardware profiles:", err);
            } finally {
                setLoading(false);
            }
        };
        fetchProfiles();
    }, []);

    return { hardwareProfiles, loading };
}
