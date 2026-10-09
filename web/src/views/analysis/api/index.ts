import { defHttp } from '@/utils/http';
export interface DashboardChartCommonResp { name: string; value: number }
export interface AnalysisSnapshot {
  days: number; from: string; through: string; updatedAt: string; scope: string; canManageAll: boolean;
  summary: Record<string, number>;
  trend: {name:string; operations:number; logins:number; albumVisits:number}[];
  browser: DashboardChartCommonResp[]; os: DashboardChartCommonResp[]; geo: DashboardChartCommonResp[];
  modules: DashboardChartCommonResp[]; timeslots: DashboardChartCommonResp[];
}
let days=30, pending:Promise<AnalysisSnapshot>|undefined;
export function resetAnalysis(period:number){days=period;pending=undefined;}
export function getOverview(){
  if(!pending)pending=defHttp.get<AnalysisSnapshot>({url:'/analysis/overview',params:{days}}).catch(error=>{pending=undefined;throw error;});
  return pending;
}
export const getAnalysisBrowser=async()=>({data:(await getOverview()).browser});
export const getAnalysisOs=async()=>({data:(await getOverview()).os});
export const getAnalysisModule=async()=>({data:(await getOverview()).modules});
export const getAnalysisTimeslot=async()=>({data:(await getOverview()).timeslots});
