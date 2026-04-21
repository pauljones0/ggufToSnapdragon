"use client"

import { useState } from "react"
import { MonitorSmartphone, Cpu, UploadCloud, ArrowRight, Loader2, CheckCircle, XCircle, LogIn, LogOut } from "lucide-react"
import { motion, AnimatePresence } from "framer-motion"
import { useAuth } from "@/hooks/useAuth"
import { useHardwareProfiles } from "@/hooks/useHardwareProfiles"
import { useJobTelemetry, JobStatus } from "@/hooks/useJobTelemetry"

export default function Home() {
    const { user, authLoading, login, logout } = useAuth()
    const { hardwareProfiles, loading: profilesLoading } = useHardwareProfiles()

    const [hfUrl, setHfUrl] = useState("")
    const [phoneModel, setPhoneModel] = useState("")
    const [selectedPhoneLimit, setSelectedPhoneLimit] = useState<number | null>(null)
    const [jobId, setJobId] = useState("")
    const [jobCreatedAt, setJobCreatedAt] = useState<Date | null>(null)

    // Custom telemetry hook abstraction
    const { status, setStatus, errorMessage, setErrorMessage, queuePosition, setQueuePosition } = useJobTelemetry(jobId, "idle", jobCreatedAt)

    // Derived specific states to map to the UI steps
    const isQueued = status === "queued" || status === "provisioning" || status === "downloading" || status === "compiling" || status === "uploading" || status === "completed"
    const isProvisioning = status === "provisioning" || status === "downloading" || status === "compiling" || status === "uploading" || status === "completed"
    const isCompiling = status === "downloading" || status === "compiling" || status === "uploading" || status === "completed"
    const isUploading = status === "uploading" || status === "completed"
    const isDone = status === "completed"
    const isFailed = status === "failed"

    const handleLogout = async () => {
        await logout()
        setJobId("")
        setStatus("idle")
    }

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault()
        if (!user) return

        setStatus("loading")
        setErrorMessage("")
        setJobId("")
        setQueuePosition(null)
        setJobCreatedAt(null)

        try {
            const token = await user.getIdToken()
            const gcFunctionUrl = process.env.NEXT_PUBLIC_SUBMIT_JOB_URL || "http://localhost:8080/submitJob"
            const traceId = crypto.randomUUID()

            const res = await fetch(gcFunctionUrl, {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    "Authorization": `Bearer ${token}`,
                    "X-Trace-ID": traceId
                },
                body: JSON.stringify({
                    user_id: user.uid,
                    phone_model: phoneModel,
                    hf_url: hfUrl
                })
            })

            if (!res.ok) {
                const errorText = await res.text()
                throw new Error(errorText)
            }

            const data = await res.json()
            setJobId(data.job_id)
            setJobCreatedAt(new Date(data.created_at))
            setStatus("queued")
        } catch (err: any) {
            setErrorMessage(err.message)
            setStatus("failed")
        }
    }

    // Regex for frontend instant validation
    let exceedsSizeLimit = false
    let extractedSize = 0
    if (selectedPhoneLimit !== null && hfUrl) {
        const match = hfUrl.match(/([\d.]+)[bB]/)
        if (match && match[1]) {
            extractedSize = parseFloat(match[1])
            if (extractedSize > selectedPhoneLimit) {
                exceedsSizeLimit = true
            }
        }
    }

    return (
        <main className="min-h-screen bg-[#050505] text-slate-300 relative overflow-hidden font-sans selection:bg-emerald-500/30">
            <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[800px] h-[800px] bg-emerald-900/20 rounded-full blur-[120px] pointer-events-none" />

            {/* Navigation / Auth */}
            <nav className="absolute top-0 w-full p-6 flex justify-end z-20">
                {!authLoading && (
                    user ? (
                        <div className="flex items-center gap-4">
                            <span className="text-sm text-slate-400">Welcome, {user.displayName}</span>
                            <button onClick={handleLogout} className="text-sm px-4 py-2 bg-white/5 hover:bg-white/10 rounded-full border border-white/10 transition-colors flex items-center gap-2">
                                <LogOut className="w-4 h-4" /> Sign Out
                            </button>
                        </div>
                    ) : (
                        <button onClick={login} className="text-sm px-6 py-2 bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-400 rounded-full border border-emerald-500/20 transition-colors flex items-center gap-2">
                            <LogIn className="w-4 h-4" /> Sign in with Google
                        </button>
                    )
                )}
            </nav>

            <div className="max-w-4xl mx-auto px-6 py-24 relative z-10 mt-10">
                <div className="text-center mb-16 space-y-4">
                    <div className="inline-flex items-center justify-center p-3 bg-emerald-500/10 rounded-2xl mb-4 border border-emerald-500/20">
                        <Cpu className="w-8 h-8 text-emerald-400" />
                    </div>
                    <h1 className="text-5xl font-extrabold tracking-tight text-white">HexForge.<span className="text-emerald-500">Matrix</span></h1>
                    <p className="text-lg text-slate-400 max-w-2xl mx-auto">
                        Cloud-native pipeline resolving GGUF structures into Qualcomm Hexagon execution graphs for Snapdragon NPUs.
                    </p>
                </div>

                <div className="grid md:grid-cols-5 gap-8">
                    <div className="md:col-span-3 glass-panel rounded-3xl p-8 space-y-6 border border-white/10 bg-black/40 backdrop-blur-2xl">
                        <form onSubmit={handleSubmit} className="space-y-6">
                            <div className="space-y-2">
                                <label className="text-sm font-medium text-slate-300 flex items-center gap-2">
                                    <MonitorSmartphone className="w-4 h-4 text-emerald-500" /> Target Snapdragon Device
                                </label>
                                <select
                                    required
                                    value={phoneModel}
                                    onChange={(e) => {
                                        setPhoneModel(e.target.value)
                                        const profile = hardwareProfiles.find(p => p.phone_model === e.target.value)
                                        setSelectedPhoneLimit(profile?.max_npu_billions || null)
                                    }}
                                    disabled={!user || status === "loading" || isQueued || profilesLoading}
                                    className="w-full bg-white/5 border border-white/10 rounded-xl px-4 py-3 text-white focus:outline-none focus:ring-2 focus:ring-emerald-500/50 appearance-none transition-all disabled:opacity-50"
                                >
                                    <option value="" disabled>{profilesLoading ? "Loading devices..." : "Select a device mapping..."}</option>
                                    {hardwareProfiles.map(p => <option key={p.phone_model} value={p.phone_model}>{p.phone_model} (Max {p.max_npu_billions}B)</option>)}
                                </select>
                            </div>

                            <div className="space-y-2">
                                <label className="text-sm font-medium text-slate-300 flex items-center gap-2">
                                    <UploadCloud className="w-4 h-4 text-emerald-500" /> Hugging Face GGUF Link
                                </label>
                                <input
                                    type="url"
                                    required
                                    value={hfUrl}
                                    onChange={(e) => setHfUrl(e.target.value)}
                                    disabled={!user || status === "loading" || isQueued}
                                    placeholder="https://huggingface.co/username/model-name/..."
                                    className="w-full bg-white/5 border border-white/10 rounded-xl px-4 py-3 text-white placeholder-slate-600 focus:outline-none focus:ring-2 focus:ring-emerald-500/50 transition-all disabled:opacity-50"
                                />
                            </div>

                            {exceedsSizeLimit && (
                                <div className="p-4 bg-amber-500/10 border border-amber-500/20 rounded-xl text-amber-400 text-sm flex items-start gap-3">
                                    <XCircle className="w-5 h-5 shrink-0 mt-0.5" />
                                    <p>
                                        <strong className="block mb-1">Model Too Large ({extractedSize}B parameters)</strong>
                                        The specified Snapdragon NPU supports a maximum of {selectedPhoneLimit}B parameters.
                                        Please find a smaller variant (or heavier quantization) before attempting to compile.
                                    </p>
                                </div>
                            )}

                            <button
                                type="submit"
                                disabled={!user || (status !== "idle" && status !== "failed" && status !== "completed") || exceedsSizeLimit}
                                className="w-full bg-emerald-500 hover:bg-emerald-400 hover:scale-[1.02] active:scale-[0.98] hover:shadow-[0_0_20px_rgba(16,185,129,0.4)] text-black font-bold tracking-wide rounded-xl px-4 py-4 transition-all duration-300 flex items-center justify-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:scale-100 disabled:hover:shadow-none group relative overflow-hidden"
                            >
                                {!user ? "Sign in to compile" : status === "loading" ? <Loader2 className="w-5 h-5 animate-spin" /> : "Init Sequence"}
                                {user && status !== "loading" && <ArrowRight className="w-5 h-5 group-hover:translate-x-1 transition-transform" />}
                            </button>
                        </form>

                        {isFailed && (
                            <div className="p-4 bg-red-500/10 border border-red-500/20 rounded-xl text-red-400 text-sm flex items-start gap-3">
                                <XCircle className="w-5 h-5 shrink-0 mt-0.5" />
                                <p>{errorMessage}</p>
                            </div>
                        )}
                    </div>

                    <div className="md:col-span-2 glass-panel rounded-3xl p-8 border border-white/10 bg-black/40 backdrop-blur-2xl flex flex-col h-full relative overflow-hidden">
                        <h3 className="text-white font-semibold mb-6 flex items-center justify-between">
                            Pipeline Telemetry
                            {status !== "idle" && status !== "failed" && !isDone && (
                                <span className="flex items-center gap-2 text-xs px-3 py-1 bg-emerald-500/10 text-emerald-400 rounded-full border border-emerald-500/20 animate-pulse-slow">
                                    <div className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" /> Live
                                </span>
                            )}
                        </h3>

                        {(!user || status === "idle" || status === "failed") && (
                            <div className="flex-1 flex items-center justify-center text-slate-500 text-sm italic text-center animate-fade-in">
                                Awaiting compilation job.
                            </div>
                        )}

                        {user && status !== "idle" && status !== "failed" && (
                            <div className="space-y-8 flex-1 flex flex-col justify-center animate-fade-in relative z-10">
                                <AnimatePresence mode="popLayout">
                                    {status === "queued" && queuePosition !== null && (
                                        <motion.div
                                            initial={{ opacity: 0, y: 20 }}
                                            animate={{ opacity: 1, y: 0 }}
                                            exit={{ opacity: 0, scale: 0.95 }}
                                            className="mb-8 p-6 rounded-2xl bg-[#030303]/60 border border-white/10 text-center shadow-2xl relative overflow-hidden group backdrop-blur-xl"
                                        >
                                            <div className="absolute inset-0 bg-amber-500/5 group-hover:bg-amber-500/10 transition-colors duration-500 -z-10" />
                                            <div className="text-xs text-slate-400 uppercase tracking-widest font-bold mb-6">Live Drone Queue</div>

                                            <div className="flex flex-wrap justify-center mb-6 max-w-[280px] mx-auto pt-2 pb-4">
                                                {Array.from({ length: Math.min(queuePosition + 3, 20) }, (_, i) => i + 1).map((hex) => {
                                                    const isUserNode = hex === queuePosition;
                                                    const isAhead = hex < queuePosition;

                                                    let bgColor = "bg-slate-800/80 border-white/5";
                                                    let innerText = "";
                                                    if (isUserNode) {
                                                        bgColor = "bg-amber-500/90 border-amber-400 shadow-[0_0_15px_rgba(245,158,11,0.6)]";
                                                        innerText = hex.toString();
                                                    } else if (isAhead) {
                                                        bgColor = "bg-emerald-500/20 border-emerald-500/50";
                                                    }

                                                    return (
                                                        <motion.div
                                                            key={hex}
                                                            initial={{ scale: 0 }}
                                                            animate={{ scale: 1 }}
                                                            transition={{ delay: hex * 0.03, type: "spring" }}
                                                            className={`relative w-8 h-9 ${bgColor} border flex items-center justify-center transition-colors duration-500`}
                                                            style={{
                                                                clipPath: "polygon(50% 0%, 100% 25%, 100% 75%, 50% 100%, 0% 75%, 0% 25%)",
                                                                marginLeft: hex % 2 === 0 ? '4px' : '4px',
                                                                marginTop: hex > 1 ? '-8px' : '0'
                                                            }}
                                                        >
                                                            {isUserNode && <span className="text-[12px] font-black text-black z-10 block">{innerText}</span>}
                                                            {isUserNode && <div className="absolute inset-0 bg-amber-400 animate-ping opacity-50 block" style={{ animationDuration: '2s' }} />}
                                                        </motion.div>
                                                    )
                                                })}
                                            </div>

                                            {queuePosition > 17 && (
                                                <div className="text-xs text-amber-500/70 mb-4 font-mono animate-pulse">
                                                    +{queuePosition - 17} nodes active in matrix...
                                                </div>
                                            )}

                                            <div className="inline-flex items-center gap-2 text-xs font-bold px-4 py-2 bg-amber-500/10 text-amber-400 rounded-lg border border-amber-500/20 shadow-[0_0_15px_rgba(245,158,11,0.1)]">
                                                Est. Wait: ~{queuePosition * 2} mins
                                            </div>
                                        </motion.div>
                                    )}
                                </AnimatePresence>

                                <div className="space-y-6 relative before:absolute before:inset-0 before:ml-[5px] before:-translate-x-px before:h-full before:w-[2px] before:bg-gradient-to-b before:from-transparent before:via-white/10 before:to-transparent">
                                    <StatusStep label="Authenticating Request" isActive={status === "loading"} isDone={isQueued} />
                                    <StatusStep label="Queue Manager (Pending)" isActive={status === "queued"} isDone={isProvisioning} />
                                    <StatusStep label="Worker Provisioning" isActive={status === "provisioning"} isDone={isCompiling} />
                                    <StatusStep label="Cross-compiling Graph" isActive={status === "downloading" || status === "compiling"} isDone={isUploading} />
                                    <StatusStep label="Uploading Container" isActive={status === "uploading"} isDone={isDone} />
                                </div>
                            </div>
                        )}

                        {isDone && (
                            <div className="mt-8 pt-6 border-t border-white/10">
                                <p className="text-emerald-400 font-medium mb-2 flex items-center gap-2"><CheckCircle className="w-5 h-5" /> Compilation Complete</p>
                                <p className="text-sm text-slate-400">
                                    Target model deployed. <br />
                                    <a href={`https://huggingface.co/pauljones0`} target="_blank" className="text-white hover:text-emerald-400 underline decoration-white/20 underline-offset-4 transition-colors">View on Hugging Face</a>
                                </p>

                                <button
                                    onClick={() => { setStatus("idle"); setJobId(""); setHfUrl(""); }}
                                    className="mt-4 w-full py-2 bg-white/5 hover:bg-white/10 rounded-lg text-sm text-white transition-colors border border-white/10"
                                >
                                    Start New Job
                                </button>
                            </div>
                        )}
                    </div>
                </div>
            </div>
        </main>
    )
}

function StatusStep({ label, isActive, isDone }: { label: string, isActive: boolean, isDone: boolean }) {
    return (
        <motion.div
            initial={{ opacity: 0, x: -10 }}
            animate={{ opacity: isActive || isDone ? 1 : 0.4, x: isActive ? 12 : 0 }}
            transition={{ duration: 0.5, ease: "easeOut" }}
            className="relative flex items-center gap-4"
        >
            <motion.div
                animate={{
                    scale: isActive ? [1, 1.2, 1] : 1,
                    backgroundColor: isDone ? '#10b981' : isActive ? '#fbbf24' : '#334155'
                }}
                transition={{ duration: isActive ? 2 : 0.5, repeat: isActive ? Infinity : 0 }}
                className={`w-3 h-3 rounded-full shrink-0 z-10 ${isDone ? 'shadow-[0_0_15px_rgba(16,185,129,0.5)]' : isActive ? 'neon-glow' : 'border border-white/20'}`}
            />
            <span className={`text-sm tracking-wide transition-colors duration-300 ${isActive ? 'text-white font-semibold' : isDone ? 'text-emerald-400 font-medium' : 'text-slate-400'}`}>
                {label}
            </span>
            <AnimatePresence>
                {isActive && (
                    <motion.div
                        initial={{ opacity: 0, width: 0 }}
                        animate={{ opacity: 1, width: '75%' }}
                        exit={{ opacity: 0, width: 0 }}
                        transition={{ duration: 0.8 }}
                        className="absolute left-3 top-1/2 -translate-y-1/2 h-8 bg-gradient-to-r from-amber-400/10 to-transparent blur-xl -z-10 rounded-full"
                    />
                )}
            </AnimatePresence>
        </motion.div>
    )
}
