'use client';

interface TerminalBlockProps {
  commands: { comment: string; command: string }[];
}

export default function TerminalBlock({ commands }: TerminalBlockProps) {
  return (
    <div className="terminal overflow-hidden">
      <div className="terminal-dots">
        <span className="terminal-dot bg-red-500" />
        <span className="terminal-dot bg-yellow-500" />
        <span className="terminal-dot bg-green-500" />
      </div>
      <div className="px-4 pb-4 pt-2 overflow-x-auto text-sm sm:text-base">
        {commands.map((cmd, i) => (
          <div key={i} className="mb-3 last:mb-0">
            <div className="text-eth-dim">{cmd.comment}</div>
            <div className="text-eth-teal">
              <span className="text-eth-purple mr-2">$</span>
              {cmd.command}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
