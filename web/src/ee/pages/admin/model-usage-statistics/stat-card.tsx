// StatCard component for displaying statistics

interface StatCardProps {
  title: string;
  value: string | number;
  subtitle?: string;
  // icon: React.ReactNode;
}

export function StatCard({ title, value, subtitle }: StatCardProps) {
  return (
    <div className="border-0.5 border-border-button bg-bg-card rounded-lg p-4">
      <div className="flex items-center justify-between">
        <div className="flex-1">
          <p className="text-xs text-text-secondary mb-2">{title}</p>
          <p className="text-2xl font-bold text-text-primary">{value}</p>
          {subtitle && (
            <p className="text-xs text-text-secondary mt-1">{subtitle}</p>
          )}
        </div>
        {/* <div className="size-10 rounded-lg bg-accent-primary/10 flex items-center justify-center text-accent-primary">
          {icon}
        </div> */}
      </div>
    </div>
  );
}
