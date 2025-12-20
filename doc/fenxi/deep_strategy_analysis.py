#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
深度策略分析 - 价格与持仓关系、交易时机分析
"""

import pandas as pd
import numpy as np
import glob
import os

def analyze_price_position_relationship(df, cycle_start_time):
    """分析价格与持仓的关系"""
    df['相对时间'] = df['时间戳'] - cycle_start_time
    df['相对分钟'] = df['相对时间'] / 60.0
    
    # 获取最终价格和胜负
    final_up_price = None
    for idx in range(len(df)-1, -1, -1):
        row = df.iloc[idx]
        if pd.notna(row['价格']) and row['方向'] == 'Up':
            final_up_price = row['价格']
            break
    
    if final_up_price is None:
        final_down_price = None
        for idx in range(len(df)-1, -1, -1):
            row = df.iloc[idx]
            if pd.notna(row['价格']) and row['方向'] == 'Down':
                final_down_price = row['价格']
                break
        if final_down_price is not None:
            final_up_price = 1.0 - final_down_price
    
    is_up_win = final_up_price >= 0.50 if final_up_price else False
    
    # 分析每个时间段的价格与持仓关系
    period_0_5 = df[df['时间段'] == '0-5分钟']
    period_5_10 = df[df['时间段'] == '5-10分钟']
    period_10_15 = df[df['时间段'] == '10-15分钟']
    
    analysis = {
        '周期开始时间': cycle_start_time,
        '最终利润': df.iloc[-1]['利润'],
        '最终UP价格': final_up_price,
        '胜负方向': 'UP' if is_up_win else 'DOWN',
    }
    
    # 分析阶段1：价格与持仓的关系
    if len(period_0_5) > 0:
        up_trades_0_5 = period_0_5[period_0_5['方向'] == 'Up']
        down_trades_0_5 = period_0_5[period_0_5['方向'] == 'Down']
        
        if len(up_trades_0_5) > 0:
            analysis['阶段1_UP价格均值'] = up_trades_0_5['价格'].mean()
            analysis['阶段1_UP价格中位数'] = up_trades_0_5['价格'].median()
            analysis['阶段1_UP价格最小值'] = up_trades_0_5['价格'].min()
            analysis['阶段1_UP价格最大值'] = up_trades_0_5['价格'].max()
            analysis['阶段1_UP总买入量'] = up_trades_0_5['数量'].sum()
            analysis['阶段1_UP总成本'] = up_trades_0_5['USDC金额'].sum()
            # 计算价格加权平均持仓成本
            analysis['阶段1_UP加权均价'] = (up_trades_0_5['价格'] * up_trades_0_5['数量']).sum() / up_trades_0_5['数量'].sum() if up_trades_0_5['数量'].sum() > 0 else None
        
        if len(down_trades_0_5) > 0:
            analysis['阶段1_DOWN价格均值'] = down_trades_0_5['价格'].mean()
            analysis['阶段1_DOWN价格中位数'] = down_trades_0_5['价格'].median()
            analysis['阶段1_DOWN价格最小值'] = down_trades_0_5['价格'].min()
            analysis['阶段1_DOWN价格最大值'] = down_trades_0_5['价格'].max()
            analysis['阶段1_DOWN总买入量'] = down_trades_0_5['数量'].sum()
            analysis['阶段1_DOWN总成本'] = down_trades_0_5['USDC金额'].sum()
            analysis['阶段1_DOWN加权均价'] = (down_trades_0_5['价格'] * down_trades_0_5['数量']).sum() / down_trades_0_5['数量'].sum() if down_trades_0_5['数量'].sum() > 0 else None
    
    # 分析阶段2
    if len(period_5_10) > 0:
        up_trades_5_10 = period_5_10[period_5_10['方向'] == 'Up']
        down_trades_5_10 = period_5_10[period_5_10['方向'] == 'Down']
        
        if len(up_trades_5_10) > 0:
            analysis['阶段2_UP价格均值'] = up_trades_5_10['价格'].mean()
            analysis['阶段2_UP价格中位数'] = up_trades_5_10['价格'].median()
            analysis['阶段2_UP价格最小值'] = up_trades_5_10['价格'].min()
            analysis['阶段2_UP价格最大值'] = up_trades_5_10['价格'].max()
            analysis['阶段2_UP总买入量'] = up_trades_5_10['数量'].sum()
            analysis['阶段2_UP总成本'] = up_trades_5_10['USDC金额'].sum()
            analysis['阶段2_UP加权均价'] = (up_trades_5_10['价格'] * up_trades_5_10['数量']).sum() / up_trades_5_10['数量'].sum() if up_trades_5_10['数量'].sum() > 0 else None
        
        if len(down_trades_5_10) > 0:
            analysis['阶段2_DOWN价格均值'] = down_trades_5_10['价格'].mean()
            analysis['阶段2_DOWN价格中位数'] = down_trades_5_10['价格'].median()
            analysis['阶段2_DOWN价格最小值'] = down_trades_5_10['价格'].min()
            analysis['阶段2_DOWN价格最大值'] = down_trades_5_10['价格'].max()
            analysis['阶段2_DOWN总买入量'] = down_trades_5_10['数量'].sum()
            analysis['阶段2_DOWN总成本'] = down_trades_5_10['USDC金额'].sum()
            analysis['阶段2_DOWN加权均价'] = (down_trades_5_10['价格'] * down_trades_5_10['数量']).sum() / down_trades_5_10['数量'].sum() if down_trades_5_10['数量'].sum() > 0 else None
    
    # 分析阶段3
    if len(period_10_15) > 0:
        up_trades_10_15 = period_10_15[period_10_15['方向'] == 'Up']
        down_trades_10_15 = period_10_15[period_10_15['方向'] == 'Down']
        
        if len(up_trades_10_15) > 0:
            analysis['阶段3_UP价格均值'] = up_trades_10_15['价格'].mean()
            analysis['阶段3_UP价格中位数'] = up_trades_10_15['价格'].median()
            analysis['阶段3_UP价格最小值'] = up_trades_10_15['价格'].min()
            analysis['阶段3_UP价格最大值'] = up_trades_10_15['价格'].max()
            analysis['阶段3_UP总买入量'] = up_trades_10_15['数量'].sum()
            analysis['阶段3_UP总成本'] = up_trades_10_15['USDC金额'].sum()
            analysis['阶段3_UP加权均价'] = (up_trades_10_15['价格'] * up_trades_10_15['数量']).sum() / up_trades_10_15['数量'].sum() if up_trades_10_15['数量'].sum() > 0 else None
        
        if len(down_trades_10_15) > 0:
            analysis['阶段3_DOWN价格均值'] = down_trades_10_15['价格'].mean()
            analysis['阶段3_DOWN价格中位数'] = down_trades_10_15['价格'].median()
            analysis['阶段3_DOWN价格最小值'] = down_trades_10_15['价格'].min()
            analysis['阶段3_DOWN价格最大值'] = down_trades_10_15['价格'].max()
            analysis['阶段3_DOWN总买入量'] = down_trades_10_15['数量'].sum()
            analysis['阶段3_DOWN总成本'] = down_trades_10_15['USDC金额'].sum()
            analysis['阶段3_DOWN加权均价'] = (down_trades_10_15['价格'] * down_trades_10_15['数量']).sum() / down_trades_10_15['数量'].sum() if down_trades_10_15['数量'].sum() > 0 else None
    
    # 分析价格趋势与持仓调整的关系
    # 计算每个阶段的价格变化
    if len(period_0_5) > 0:
        up_trades_0_5 = period_0_5[period_0_5['方向'] == 'Up']
        if len(up_trades_0_5) > 1:
            up_start_price_0_5 = up_trades_0_5['价格'].iloc[0]
            up_end_price_0_5 = up_trades_0_5['价格'].iloc[-1]
            analysis['阶段1_UP价格变化'] = up_end_price_0_5 - up_start_price_0_5
    
    if len(period_5_10) > 0:
        up_trades_5_10 = period_5_10[period_5_10['方向'] == 'Up']
        if len(up_trades_5_10) > 1:
            up_start_price_5_10 = up_trades_5_10['价格'].iloc[0]
            up_end_price_5_10 = up_trades_5_10['价格'].iloc[-1]
            analysis['阶段2_UP价格变化'] = up_end_price_5_10 - up_start_price_5_10
    
    if len(period_10_15) > 0:
        up_trades_10_15 = period_10_15[period_10_15['方向'] == 'Up']
        if len(up_trades_10_15) > 1:
            up_start_price_10_15 = up_trades_10_15['价格'].iloc[0]
            up_end_price_10_15 = up_trades_10_15['价格'].iloc[-1]
            analysis['阶段3_UP价格变化'] = up_end_price_10_15 - up_start_price_10_15
    
    # 分析持仓调整与价格的关系
    last_0_5 = period_0_5.iloc[-1] if len(period_0_5) > 0 else None
    last_5_10 = period_5_10.iloc[-1] if len(period_5_10) > 0 else None
    last_10_15 = period_10_15.iloc[-1] if len(period_10_15) > 0 else None
    
    if last_0_5 is not None and last_5_10 is not None:
        up_pos_change_2 = (last_5_10['up持仓量'] if pd.notna(last_5_10['up持仓量']) else 0) - (last_0_5['up持仓量'] if pd.notna(last_0_5['up持仓量']) else 0)
        down_pos_change_2 = (last_5_10['down持仓量'] if pd.notna(last_5_10['down持仓量']) else 0) - (last_0_5['down持仓量'] if pd.notna(last_0_5['down持仓量']) else 0)
        analysis['阶段2_UP持仓变化'] = up_pos_change_2
        analysis['阶段2_DOWN持仓变化'] = down_pos_change_2
    
    if last_5_10 is not None and last_10_15 is not None:
        up_pos_change_3 = (last_10_15['up持仓量'] if pd.notna(last_10_15['up持仓量']) else 0) - (last_5_10['up持仓量'] if pd.notna(last_5_10['up持仓量']) else 0)
        down_pos_change_3 = (last_10_15['down持仓量'] if pd.notna(last_10_15['down持仓量']) else 0) - (last_5_10['down持仓量'] if pd.notna(last_5_10['down持仓量']) else 0)
        analysis['阶段3_UP持仓变化'] = up_pos_change_3
        analysis['阶段3_DOWN持仓变化'] = down_pos_change_3
    
    # 分析交易频率
    analysis['阶段1_交易频率'] = len(period_0_5) / 5.0 if len(period_0_5) > 0 else 0  # 每分钟交易次数
    analysis['阶段2_交易频率'] = len(period_5_10) / 5.0 if len(period_5_10) > 0 else 0
    analysis['阶段3_交易频率'] = len(period_10_15) / 5.0 if len(period_10_15) > 0 else 0
    
    # 分析是否在低价时买入更多（价格敏感度）
    if len(period_0_5) > 0:
        up_trades = period_0_5[period_0_5['方向'] == 'Up']
        if len(up_trades) > 1:
            # 计算价格与买入量的相关性
            correlation = up_trades['价格'].corr(up_trades['数量'])
            analysis['阶段1_UP价格数量相关性'] = correlation
            # 如果相关性为负，说明价格越低买入越多
    
    return analysis

def analyze_trading_patterns(df_all):
    """分析交易模式"""
    profitable = df_all[df_all['最终利润'] > 0]
    unprofitable = df_all[df_all['最终利润'] <= 0]
    
    print("\n" + "="*80)
    print("【价格与持仓关系深度分析】")
    print("="*80)
    
    print("\n【阶段1（0-5分钟）- 基础建仓价格策略】")
    if len(profitable) > 0:
        print("\n盈利周期:")
        print(f"  UP平均买入价格: {profitable['阶段1_UP价格均值'].mean():.4f}")
        print(f"  UP价格范围: {profitable['阶段1_UP价格最小值'].min():.4f} - {profitable['阶段1_UP价格最大值'].max():.4f}")
        print(f"  DOWN平均买入价格: {profitable['阶段1_DOWN价格均值'].mean():.4f}")
        print(f"  DOWN价格范围: {profitable['阶段1_DOWN价格最小值'].min():.4f} - {profitable['阶段1_DOWN价格最大值'].max():.4f}")
        print(f"  UP平均持仓量: {profitable['阶段1_UP总买入量'].mean():.1f}")
        print(f"  DOWN平均持仓量: {profitable['阶段1_DOWN总买入量'].mean():.1f}")
    
    print("\n【阶段2（5-10分钟）- 价格调整策略】")
    if len(profitable) > 0:
        print("\n盈利周期:")
        print(f"  UP平均买入价格: {profitable['阶段2_UP价格均值'].mean():.4f}")
        print(f"  DOWN平均买入价格: {profitable['阶段2_DOWN价格均值'].mean():.4f}")
        print(f"  UP持仓变化: {profitable['阶段2_UP持仓变化'].mean():+.1f}")
        print(f"  DOWN持仓变化: {profitable['阶段2_DOWN持仓变化'].mean():+.1f}")
    
    print("\n【阶段3（10-15分钟）- 锁定方向价格策略】")
    if len(profitable) > 0:
        print("\n盈利周期:")
        print(f"  UP平均买入价格: {profitable['阶段3_UP价格均值'].mean():.4f}")
        print(f"  DOWN平均买入价格: {profitable['阶段3_DOWN价格均值'].mean():.4f}")
        print(f"  UP持仓变化: {profitable['阶段3_UP持仓变化'].mean():+.1f}")
        print(f"  DOWN持仓变化: {profitable['阶段3_DOWN持仓变化'].mean():+.1f}")
    
    print("\n【交易频率分析】")
    print(f"阶段1平均交易频率: {df_all['阶段1_交易频率'].mean():.1f} 次/分钟")
    print(f"阶段2平均交易频率: {df_all['阶段2_交易频率'].mean():.1f} 次/分钟")
    print(f"阶段3平均交易频率: {df_all['阶段3_交易频率'].mean():.1f} 次/分钟")
    
    if len(profitable) > 0:
        print("\n盈利周期交易频率:")
        print(f"  阶段1: {profitable['阶段1_交易频率'].mean():.1f} 次/分钟")
        print(f"  阶段2: {profitable['阶段2_交易频率'].mean():.1f} 次/分钟")
        print(f"  阶段3: {profitable['阶段3_交易频率'].mean():.1f} 次/分钟")
    
    print("\n【价格敏感度分析】")
    if '阶段1_UP价格数量相关性' in df_all.columns:
        avg_corr = df_all['阶段1_UP价格数量相关性'].mean()
        print(f"阶段1 UP价格与买入量相关性: {avg_corr:.4f}")
        if avg_corr < -0.3:
            print("  → 策略在UP价格较低时买入更多（价值投资倾向）")
        elif avg_corr > 0.3:
            print("  → 策略在UP价格较高时买入更多（追涨倾向）")
        else:
            print("  → 策略买入量与价格关系不明显（平衡策略）")

def generate_strategy_rules(df_all):
    """生成可复刻的策略规则"""
    profitable = df_all[df_all['最终利润'] > 0]
    
    print("\n" + "="*80)
    print("【策略复刻规则总结】")
    print("="*80)
    
    print("\n【核心策略原则】")
    print("1. 双向套利策略：始终保持UP和DOWN两个方向的持仓")
    print("2. 动态调整：根据价格变化和市场情况动态调整两个方向的持仓比例")
    print("3. 风险对冲：通过双向持仓降低单边风险")
    
    print("\n【阶段1（0-5分钟）- 基础建仓规则】")
    print(f"  目标持仓比例: UP {df_all['阶段1_持仓比例'].mean()*100:.1f}% / DOWN {(1-df_all['阶段1_持仓比例'].mean())*100:.1f}%")
    print(f"  目标UP持仓量: {df_all['阶段1_UP总买入量'].mean():.0f} 单位")
    print(f"  目标DOWN持仓量: {df_all['阶段1_DOWN总买入量'].mean():.0f} 单位")
    print(f"  交易频率: {df_all['阶段1_交易频率'].mean():.1f} 次/分钟")
    if len(profitable) > 0:
        print(f"  UP买入价格范围: {profitable['阶段1_UP价格最小值'].min():.3f} - {profitable['阶段1_UP价格最大值'].max():.3f}")
        print(f"  DOWN买入价格范围: {profitable['阶段1_DOWN价格最小值'].min():.3f} - {profitable['阶段1_DOWN价格最大值'].max():.3f}")
    
    print("\n【阶段2（5-10分钟）- 调整仓位规则】")
    print(f"  平均持仓比例变化: {df_all['阶段2_持仓比例变化'].mean()*100:+.2f}%")
    print(f"  交易频率: {df_all['阶段2_交易频率'].mean():.1f} 次/分钟")
    if len(profitable) > 0:
        avg_up_change = profitable['阶段2_UP持仓变化'].mean()
        avg_down_change = profitable['阶段2_DOWN持仓变化'].mean()
        print(f"  平均UP持仓变化: {avg_up_change:+.1f}")
        print(f"  平均DOWN持仓变化: {avg_down_change:+.1f}")
        if abs(avg_up_change) > abs(avg_down_change):
            print("  → 策略倾向于在阶段2增加UP方向持仓")
        elif abs(avg_down_change) > abs(avg_up_change):
            print("  → 策略倾向于在阶段2增加DOWN方向持仓")
        else:
            print("  → 策略在阶段2保持相对平衡")
    
    print("\n【阶段3（10-15分钟）- 锁定方向规则】")
    print(f"  平均持仓比例变化: {df_all['阶段3_持仓比例变化'].mean()*100:+.2f}%")
    print(f"  交易频率: {df_all['阶段3_交易频率'].mean():.1f} 次/分钟")
    if len(profitable) > 0:
        avg_up_change = profitable['阶段3_UP持仓变化'].mean()
        avg_down_change = profitable['阶段3_DOWN持仓变化'].mean()
        print(f"  平均UP持仓变化: {avg_up_change:+.1f}")
        print(f"  平均DOWN持仓变化: {avg_down_change:+.1f}")
    
    print("\n【关键策略特征】")
    print("1. 始终保持双向持仓，避免单边风险")
    print("2. 初期（0-5分钟）快速建仓，建立基础仓位")
    print("3. 中期（5-10分钟）根据市场情况调整，但变化幅度较小")
    print("4. 后期（10-15分钟）继续微调，但不会大幅改变持仓结构")
    print("5. 交易频率在初期最高，后期逐渐降低")
    
    print("\n【风险控制要点】")
    print("1. 避免在单一方向过度集中")
    print("2. 保持UP和DOWN持仓相对平衡（比例在45%-55%之间）")
    print("3. 根据价格变化动态调整，但调整幅度要控制")
    
    print("\n【盈利周期关键成功因素】")
    if len(profitable) > 0:
        print(f"1. 初期持仓比例接近50%（实际: {profitable['阶段1_持仓比例'].mean()*100:.1f}%）")
        print(f"2. 最终持仓比例接近50%（实际: {profitable['最终_持仓比例'].mean()*100:.1f}%）")
        print(f"3. 持仓调整幅度较小（阶段2变化: {profitable['阶段2_持仓比例变化'].mean()*100:+.2f}%，阶段3变化: {profitable['阶段3_持仓比例变化'].mean()*100:+.2f}%）")
    
    print("\n【亏损周期失败原因分析】")
    unprofitable = df_all[df_all['最终利润'] <= 0]
    if len(unprofitable) > 0:
        print(f"1. 初期持仓比例偏离50%（实际: {unprofitable['阶段1_持仓比例'].mean()*100:.1f}%）")
        print(f"2. 最终持仓比例偏离50%（实际: {unprofitable['最终_持仓比例'].mean()*100:.1f}%）")
        print(f"3. 持仓调整可能过度（阶段2变化: {unprofitable['阶段2_持仓比例变化'].mean()*100:+.2f}%）")

if __name__ == '__main__':
    os.chdir(os.path.dirname(os.path.abspath(__file__)))
    
    # 先读取第一个分析的结果（如果有）
    detail_file = 'strategy_analysis_detail.csv'
    if os.path.exists(detail_file):
        df_detail = pd.read_csv(detail_file)
        print(f"读取已有分析数据: {detail_file}")
    else:
        df_detail = None
    
    analyzed_files = glob.glob('bot_v0_8_cycle_*_analyzed.csv')
    all_analyses = []
    
    for file in sorted(analyzed_files):
        print(f"深度分析: {file}")
        df = pd.read_csv(file)
        cycle_start = int(file.split('_')[-2])
        analysis = analyze_price_position_relationship(df, cycle_start)
        all_analyses.append(analysis)
    
    df_all = pd.DataFrame(all_analyses)
    df_all.to_csv('deep_strategy_analysis.csv', index=False, encoding='utf-8-sig')
    
    # 如果存在详细分析数据，合并使用
    if df_detail is not None:
        # 合并数据
        df_merged = pd.merge(df_all, df_detail, on='周期开始时间', how='left', suffixes=('', '_detail'))
        analyze_trading_patterns(df_merged)
        generate_strategy_rules(df_merged)
    else:
        analyze_trading_patterns(df_all)
        # 简化策略规则生成，只使用可用的数据
        print("\n" + "="*80)
        print("【策略复刻规则总结】")
        print("="*80)
        print("\n【核心策略原则】")
        print("1. 双向套利策略：始终保持UP和DOWN两个方向的持仓")
        print("2. 动态调整：根据价格变化和市场情况动态调整两个方向的持仓比例")
        print("3. 风险对冲：通过双向持仓降低单边风险")
        print("\n【交易频率】")
        print(f"阶段1: {df_all['阶段1_交易频率'].mean():.1f} 次/分钟")
        print(f"阶段2: {df_all['阶段2_交易频率'].mean():.1f} 次/分钟")
        print(f"阶段3: {df_all['阶段3_交易频率'].mean():.1f} 次/分钟")
    
    print("\n" + "="*80)
    print("详细分析数据已保存到: deep_strategy_analysis.csv")
    print("="*80)

