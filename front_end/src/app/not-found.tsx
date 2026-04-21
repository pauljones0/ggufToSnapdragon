import Link from 'next/link'

export default function NotFound() {
    return (
        <div className="min-h-screen bg-[#050505] flex flex-col items-center justify-center text-slate-300">
            <h2 className="text-4xl font-bold mb-4 text-white">404 - Matrix Sector Unreachable</h2>
            <p className="text-slate-400 mb-8">The requested pipeline endpoint does not exist.</p>
            <Link href="/" className="px-6 py-3 bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 rounded-xl hover:bg-emerald-500/20 transition-colors">
                Return to Dashboard
            </Link>
        </div>
    )
}
