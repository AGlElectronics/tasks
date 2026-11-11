import { Link } from '@tanstack/react-router';
import { ScrollText } from 'lucide-react';
import { PageContainer, PageHeader } from '@/components/ui/PageHeader';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { DenConnection } from '@/components/DenConnection';
import { ModuleSettingsCard } from '@/components/ModuleSettingsCard';

/** Module settings: Den pairing, retention, reminders, email and the audit log. */
export function SettingsPage() {
  return (
    <PageContainer>
      <PageHeader
        title="Settings"
        description="Module settings. Only Tasks admins can see this page."
      />
      <div className="max-w-2xl space-y-6">
        <DenConnection base="/api/v1/den" />
        <ModuleSettingsCard />
        <Card>
          <CardHeader>
            <CardTitle>Audit log</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground mb-3">
              Who deleted, purged, archived or shared what, and when.
            </p>
            <Link to="/settings/audit" className="inline-flex items-center gap-1.5 text-sm font-semibold text-primary hover:underline">
              <ScrollText className="w-4 h-4" /> Open the audit log
            </Link>
          </CardContent>
        </Card>
      </div>
    </PageContainer>
  );
}
