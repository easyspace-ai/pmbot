#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
深度分析交易策略模式
"""

import pandas as pd
import numpy as np
import glob
import os
from collections import defaultdict

def analyze_cycle_strategy(df, cycle_start_time):
    """分析单个周期的策略模式"""
    # 计算相对时间（秒）
    df['相对时间'] = df['时间戳'] - cycle_start_time
    df['相对分钟'] = df['相对时间'] / 60.0
    
    # 获取每个时间段的数据
    period_0_5 = df[df['时间段'] == '0-5分钟']
    period_5_10 = df[df['时间段'] == '5-10分钟']
    period_10_15 = df[df['时间段'] == '10-15分钟']
    
    # 获取每个时间段的最后一行（累计值）
    last_0_5 = period_0_5.iloc[-1] if len(period_0_5) > 0 else None
    last_5_10 = period_5_10.iloc[-1] if len(period_5_10) > 0 else None
    last_10_15 = period_10_15.iloc[-1] if len(period_10_15) > 0 else None
    final_row = df.iloc[-1]
    
    # 获取最终价格（最后一行的UP价格，如果没有UP则用DOWN价格推断）
    final_up_price = None
    final_down_price = None
    for idx in range(len(df)-1, -1, -1):
        row = df.iloc[idx]
        if pd.notna(row['价格']) and row['方向'] == 'Up':
            final_up_price = row['价格']
            break
        elif pd.notna(row['价格']) and row['方向'] == 'Down':
            final_down_price = row['价格']
    
    if final_up_price is None and final_down_price is not None:
        final_up_price = 1.0 - final_down_price  # 推断UP价格
    
    # 判断胜负
    is_up_win = final_up_price >= 0.50 if final_up_price else False
    final_profit = final_row['利润']
    
    # 计算每个时间段的持仓变化
    analysis = {
        '周期开始时间': cycle_start_time,
        '最终利润': final_profit,
        '最终UP价格': final_up_price,
        '胜负方向': 'UP' if is_up_win else 'DOWN',
        '总交易次数': len(df),
    }
    
    # 0-5分钟阶段分析
    if last_0_5 is not None:
        analysis['阶段1_UP持仓'] = last_0_5['up持仓量'] if pd.notna(last_0_5['up持仓量']) else 0
        analysis['阶段1_DOWN持仓'] = last_0_5['down持仓量'] if pd.notna(last_0_5['down持仓量']) else 0
        analysis['阶段1_UP成本'] = last_0_5['up总成本'] if pd.notna(last_0_5['up总成本']) else 0
        analysis['阶段1_DOWN成本'] = last_0_5['down总成本'] if pd.notna(last_0_5['down总成本']) else 0
        analysis['阶段1_UP均价'] = last_0_5['up均价'] if pd.notna(last_0_5['up均价']) else None
        analysis['阶段1_DOWN均价'] = last_0_5['down均价'] if pd.notna(last_0_5['down均价']) else None
        analysis['阶段1_交易次数'] = len(period_0_5)
        analysis['阶段1_持仓比例'] = analysis['阶段1_UP持仓'] / (analysis['阶段1_UP持仓'] + analysis['阶段1_DOWN持仓']) if (analysis['阶段1_UP持仓'] + analysis['阶段1_DOWN持仓']) > 0 else 0.5
    else:
        analysis['阶段1_UP持仓'] = 0
        analysis['阶段1_DOWN持仓'] = 0
        analysis['阶段1_UP成本'] = 0
        analysis['阶段1_DOWN成本'] = 0
        analysis['阶段1_UP均价'] = None
        analysis['阶段1_DOWN均价'] = None
        analysis['阶段1_交易次数'] = 0
        analysis['阶段1_持仓比例'] = 0.5
    
    # 5-10分钟阶段分析
    if last_5_10 is not None:
        analysis['阶段2_UP持仓'] = last_5_10['up持仓量'] if pd.notna(last_5_10['up持仓量']) else 0
        analysis['阶段2_DOWN持仓'] = last_5_10['down持仓量'] if pd.notna(last_5_10['down持仓量']) else 0
        analysis['阶段2_UP成本'] = last_5_10['up总成本'] if pd.notna(last_5_10['up总成本']) else 0
        analysis['阶段2_DOWN成本'] = last_5_10['down总成本'] if pd.notna(last_5_10['down总成本']) else 0
        analysis['阶段2_UP均价'] = last_5_10['up均价'] if pd.notna(last_5_10['up均价']) else None
        analysis['阶段2_DOWN均价'] = last_5_10['down均价'] if pd.notna(last_5_10['down均价']) else None
        analysis['阶段2_交易次数'] = len(period_5_10)
        analysis['阶段2_持仓比例'] = analysis['阶段2_UP持仓'] / (analysis['阶段2_UP持仓'] + analysis['阶段2_DOWN持仓']) if (analysis['阶段2_UP持仓'] + analysis['阶段2_DOWN持仓']) > 0 else 0.5
        
        # 计算阶段2相对于阶段1的变化
        analysis['阶段2_UP持仓变化'] = analysis['阶段2_UP持仓'] - analysis['阶段1_UP持仓']
        analysis['阶段2_DOWN持仓变化'] = analysis['阶段2_DOWN持仓'] - analysis['阶段1_DOWN持仓']
        analysis['阶段2_持仓比例变化'] = analysis['阶段2_持仓比例'] - analysis['阶段1_持仓比例']
    else:
        analysis['阶段2_UP持仓'] = analysis['阶段1_UP持仓']
        analysis['阶段2_DOWN持仓'] = analysis['阶段1_DOWN持仓']
        analysis['阶段2_UP成本'] = analysis['阶段1_UP成本']
        analysis['阶段2_DOWN成本'] = analysis['阶段1_DOWN成本']
        analysis['阶段2_UP均价'] = analysis['阶段1_UP均价']
        analysis['阶段2_DOWN均价'] = analysis['阶段1_DOWN均价']
        analysis['阶段2_交易次数'] = 0
        analysis['阶段2_持仓比例'] = analysis['阶段1_持仓比例']
        analysis['阶段2_UP持仓变化'] = 0
        analysis['阶段2_DOWN持仓变化'] = 0
        analysis['阶段2_持仓比例变化'] = 0
    
    # 10-15分钟阶段分析
    if last_10_15 is not None:
        analysis['阶段3_UP持仓'] = last_10_15['up持仓量'] if pd.notna(last_10_15['up持仓量']) else 0
        analysis['阶段3_DOWN持仓'] = last_10_15['down持仓量'] if pd.notna(last_10_15['down持仓量']) else 0
        analysis['阶段3_UP成本'] = last_10_15['up总成本'] if pd.notna(last_10_15['up总成本']) else 0
        analysis['阶段3_DOWN成本'] = last_10_15['down总成本'] if pd.notna(last_10_15['down总成本']) else 0
        analysis['阶段3_UP均价'] = last_10_15['up均价'] if pd.notna(last_10_15['up均价']) else None
        analysis['阶段3_DOWN均价'] = last_10_15['down均价'] if pd.notna(last_10_15['down均价']) else None
        analysis['阶段3_交易次数'] = len(period_10_15)
        analysis['阶段3_持仓比例'] = analysis['阶段3_UP持仓'] / (analysis['阶段3_UP持仓'] + analysis['阶段3_DOWN持仓']) if (analysis['阶段3_UP持仓'] + analysis['阶段3_DOWN持仓']) > 0 else 0.5
        
        # 计算阶段3相对于阶段2的变化
        analysis['阶段3_UP持仓变化'] = analysis['阶段3_UP持仓'] - analysis['阶段2_UP持仓']
        analysis['阶段3_DOWN持仓变化'] = analysis['阶段3_DOWN持仓'] - analysis['阶段2_DOWN持仓']
        analysis['阶段3_持仓比例变化'] = analysis['阶段3_持仓比例'] - analysis['阶段2_持仓比例']
    else:
        analysis['阶段3_UP持仓'] = analysis['阶段2_UP持仓']
        analysis['阶段3_DOWN持仓'] = analysis['阶段2_DOWN持仓']
        analysis['阶段3_UP成本'] = analysis['阶段2_UP成本']
        analysis['阶段3_DOWN成本'] = analysis['阶段2_DOWN成本']
        analysis['阶段3_UP均价'] = analysis['阶段2_UP均价']
        analysis['阶段3_DOWN均价'] = analysis['阶段2_DOWN均价']
        analysis['阶段3_交易次数'] = 0
        analysis['阶段3_持仓比例'] = analysis['阶段2_持仓比例']
        analysis['阶段3_UP持仓变化'] = 0
        analysis['阶段3_DOWN持仓变化'] = 0
        analysis['阶段3_持仓比例变化'] = 0
    
    # 最终持仓
    analysis['最终_UP持仓'] = final_row['up持仓量'] if pd.notna(final_row['up持仓量']) else 0
    analysis['最终_DOWN持仓'] = final_row['down持仓量'] if pd.notna(final_row['down持仓量']) else 0
    analysis['最终_UP成本'] = final_row['up总成本'] if pd.notna(final_row['up总成本']) else 0
    analysis['最终_DOWN成本'] = final_row['down总成本'] if pd.notna(final_row['down总成本']) else 0
    analysis['最终_持仓比例'] = analysis['最终_UP持仓'] / (analysis['最终_UP持仓'] + analysis['最终_DOWN持仓']) if (analysis['最终_UP持仓'] + analysis['最终_DOWN持仓']) > 0 else 0.5
    
    # 分析价格与持仓的关系
    # 计算每个阶段的价格范围
    if len(period_0_5) > 0:
        up_prices_0_5 = period_0_5[period_0_5['方向'] == 'Up']['价格']
        down_prices_0_5 = period_0_5[period_0_5['方向'] == 'Down']['价格']
        analysis['阶段1_UP价格范围'] = f"{up_prices_0_5.min():.3f}-{up_prices_0_5.max():.3f}" if len(up_prices_0_5) > 0 else "N/A"
        analysis['阶段1_DOWN价格范围'] = f"{down_prices_0_5.min():.3f}-{down_prices_0_5.max():.3f}" if len(down_prices_0_5) > 0 else "N/A"
        analysis['阶段1_UP价格均值'] = up_prices_0_5.mean() if len(up_prices_0_5) > 0 else None
        analysis['阶段1_DOWN价格均值'] = down_prices_0_5.mean() if len(down_prices_0_5) > 0 else None
    
    if len(period_5_10) > 0:
        up_prices_5_10 = period_5_10[period_5_10['方向'] == 'Up']['价格']
        down_prices_5_10 = period_5_10[period_5_10['方向'] == 'Down']['价格']
        analysis['阶段2_UP价格范围'] = f"{up_prices_5_10.min():.3f}-{up_prices_5_10.max():.3f}" if len(up_prices_5_10) > 0 else "N/A"
        analysis['阶段2_DOWN价格范围'] = f"{down_prices_5_10.min():.3f}-{down_prices_5_10.max():.3f}" if len(down_prices_5_10) > 0 else "N/A"
        analysis['阶段2_UP价格均值'] = up_prices_5_10.mean() if len(up_prices_5_10) > 0 else None
        analysis['阶段2_DOWN价格均值'] = down_prices_5_10.mean() if len(down_prices_5_10) > 0 else None
    
    if len(period_10_15) > 0:
        up_prices_10_15 = period_10_15[period_10_15['方向'] == 'Up']['价格']
        down_prices_10_15 = period_10_15[period_10_15['方向'] == 'Down']['价格']
        analysis['阶段3_UP价格范围'] = f"{up_prices_10_15.min():.3f}-{up_prices_10_15.max():.3f}" if len(up_prices_10_15) > 0 else "N/A"
        analysis['阶段3_DOWN价格范围'] = f"{down_prices_10_15.min():.3f}-{down_prices_10_15.max():.3f}" if len(down_prices_10_15) > 0 else "N/A"
        analysis['阶段3_UP价格均值'] = up_prices_10_15.mean() if len(up_prices_10_15) > 0 else None
        analysis['阶段3_DOWN价格均值'] = down_prices_10_15.mean() if len(down_prices_10_15) > 0 else None
    
    # 分析持仓方向性（是否偏向最终获胜方向）
    if is_up_win:
        analysis['阶段1_偏向获胜方向'] = analysis['阶段1_持仓比例'] > 0.5
        analysis['阶段2_偏向获胜方向'] = analysis['阶段2_持仓比例'] > 0.5
        analysis['阶段3_偏向获胜方向'] = analysis['阶段3_持仓比例'] > 0.5
        analysis['最终_偏向获胜方向'] = analysis['最终_持仓比例'] > 0.5
    else:
        analysis['阶段1_偏向获胜方向'] = analysis['阶段1_持仓比例'] < 0.5
        analysis['阶段2_偏向获胜方向'] = analysis['阶段2_持仓比例'] < 0.5
        analysis['阶段3_偏向获胜方向'] = analysis['阶段3_持仓比例'] < 0.5
        analysis['最终_偏向获胜方向'] = analysis['最终_持仓比例'] < 0.5
    
    return analysis

def analyze_all_cycles():
    """分析所有周期"""
    # 获取所有分析后的CSV文件
    analyzed_files = glob.glob('bot_v0_8_cycle_*_analyzed.csv')
    
    all_analyses = []
    
    for file in sorted(analyzed_files):
        print(f"分析文件: {file}")
        df = pd.read_csv(file)
        
        # 从文件名提取周期开始时间
        cycle_start = int(file.split('_')[-2])
        
        analysis = analyze_cycle_strategy(df, cycle_start)
        all_analyses.append(analysis)
    
    return pd.DataFrame(all_analyses)

def generate_strategy_summary(df):
    """生成策略总结"""
    summary = {}
    
    # 基本统计
    summary['总周期数'] = len(df)
    summary['盈利周期数'] = len(df[df['最终利润'] > 0])
    summary['亏损周期数'] = len(df[df['最终利润'] <= 0])
    summary['平均利润'] = df['最终利润'].mean()
    summary['盈利周期平均利润'] = df[df['最终利润'] > 0]['最终利润'].mean()
    summary['亏损周期平均亏损'] = df[df['最终利润'] <= 0]['最终利润'].mean()
    
    # 阶段1（0-5分钟）特征
    summary['阶段1_平均持仓比例'] = df['阶段1_持仓比例'].mean()
    summary['阶段1_平均UP持仓'] = df['阶段1_UP持仓'].mean()
    summary['阶段1_平均DOWN持仓'] = df['阶段1_DOWN持仓'].mean()
    summary['阶段1_平均交易次数'] = df['阶段1_交易次数'].mean()
    
    # 阶段2（5-10分钟）特征
    summary['阶段2_平均持仓比例'] = df['阶段2_持仓比例'].mean()
    summary['阶段2_平均持仓比例变化'] = df['阶段2_持仓比例变化'].mean()
    summary['阶段2_平均交易次数'] = df['阶段2_交易次数'].mean()
    
    # 阶段3（10-15分钟）特征
    summary['阶段3_平均持仓比例'] = df['阶段3_持仓比例'].mean()
    summary['阶段3_平均持仓比例变化'] = df['阶段3_持仓比例变化'].mean()
    summary['阶段3_平均交易次数'] = df['阶段3_交易次数'].mean()
    
    # 最终持仓特征
    summary['最终_平均持仓比例'] = df['最终_持仓比例'].mean()
    
    # 盈利周期vs亏损周期对比
    profitable = df[df['最终利润'] > 0]
    unprofitable = df[df['最终利润'] <= 0]
    
    if len(profitable) > 0:
        summary['盈利周期_阶段1_平均持仓比例'] = profitable['阶段1_持仓比例'].mean()
        summary['盈利周期_阶段2_平均持仓比例'] = profitable['阶段2_持仓比例'].mean()
        summary['盈利周期_阶段3_平均持仓比例'] = profitable['阶段3_持仓比例'].mean()
        summary['盈利周期_最终_平均持仓比例'] = profitable['最终_持仓比例'].mean()
        summary['盈利周期_阶段2_平均持仓比例变化'] = profitable['阶段2_持仓比例变化'].mean()
        summary['盈利周期_阶段3_平均持仓比例变化'] = profitable['阶段3_持仓比例变化'].mean()
    
    if len(unprofitable) > 0:
        summary['亏损周期_阶段1_平均持仓比例'] = unprofitable['阶段1_持仓比例'].mean()
        summary['亏损周期_阶段2_平均持仓比例'] = unprofitable['阶段2_持仓比例'].mean()
        summary['亏损周期_阶段3_平均持仓比例'] = unprofitable['阶段3_持仓比例'].mean()
        summary['亏损周期_最终_平均持仓比例'] = unprofitable['最终_持仓比例'].mean()
        summary['亏损周期_阶段2_平均持仓比例变化'] = unprofitable['阶段2_持仓比例变化'].mean()
        summary['亏损周期_阶段3_平均持仓比例变化'] = unprofitable['阶段3_持仓比例变化'].mean()
    
    # 方向性分析
    summary['盈利周期_阶段1_偏向获胜方向比例'] = profitable['阶段1_偏向获胜方向'].mean() if len(profitable) > 0 else 0
    summary['盈利周期_阶段2_偏向获胜方向比例'] = profitable['阶段2_偏向获胜方向'].mean() if len(profitable) > 0 else 0
    summary['盈利周期_阶段3_偏向获胜方向比例'] = profitable['阶段3_偏向获胜方向'].mean() if len(profitable) > 0 else 0
    summary['盈利周期_最终_偏向获胜方向比例'] = profitable['最终_偏向获胜方向'].mean() if len(profitable) > 0 else 0
    
    return summary

def print_detailed_analysis(df, summary):
    """打印详细分析报告"""
    print("\n" + "="*80)
    print("策略深度分析报告")
    print("="*80)
    
    print("\n【基本统计】")
    print(f"总周期数: {summary['总周期数']}")
    print(f"盈利周期数: {summary['盈利周期数']} ({summary['盈利周期数']/summary['总周期数']*100:.1f}%)")
    print(f"亏损周期数: {summary['亏损周期数']} ({summary['亏损周期数']/summary['总周期数']*100:.1f}%)")
    print(f"平均利润: {summary['平均利润']:.2f} USDC")
    print(f"盈利周期平均利润: {summary['盈利周期平均利润']:.2f} USDC")
    if summary['亏损周期平均亏损'] < 0:
        print(f"亏损周期平均亏损: {summary['亏损周期平均亏损']:.2f} USDC")
    
    print("\n【阶段1（0-5分钟）- 基础建仓阶段】")
    print(f"平均持仓比例(UP): {summary['阶段1_平均持仓比例']*100:.1f}%")
    print(f"平均UP持仓量: {summary['阶段1_平均UP持仓']:.1f}")
    print(f"平均DOWN持仓量: {summary['阶段1_平均DOWN持仓']:.1f}")
    print(f"平均交易次数: {summary['阶段1_平均交易次数']:.1f}")
    
    print("\n【阶段2（5-10分钟）- 调整仓位阶段】")
    print(f"平均持仓比例(UP): {summary['阶段2_平均持仓比例']*100:.1f}%")
    print(f"平均持仓比例变化: {summary['阶段2_平均持仓比例变化']*100:+.1f}%")
    print(f"平均交易次数: {summary['阶段2_平均交易次数']:.1f}")
    
    print("\n【阶段3（10-15分钟）- 锁定方向阶段】")
    print(f"平均持仓比例(UP): {summary['阶段3_平均持仓比例']*100:.1f}%")
    print(f"平均持仓比例变化: {summary['阶段3_平均持仓比例变化']*100:+.1f}%")
    print(f"平均交易次数: {summary['阶段3_平均交易次数']:.1f}")
    
    print("\n【盈利周期 vs 亏损周期对比】")
    if summary['盈利周期数'] > 0:
        print("\n盈利周期特征:")
        print(f"  阶段1持仓比例: {summary['盈利周期_阶段1_平均持仓比例']*100:.1f}%")
        print(f"  阶段2持仓比例: {summary['盈利周期_阶段2_平均持仓比例']*100:.1f}% (变化: {summary['盈利周期_阶段2_平均持仓比例变化']*100:+.1f}%)")
        print(f"  阶段3持仓比例: {summary['盈利周期_阶段3_平均持仓比例']*100:.1f}% (变化: {summary['盈利周期_阶段3_平均持仓比例变化']*100:+.1f}%)")
        print(f"  最终持仓比例: {summary['盈利周期_最终_平均持仓比例']*100:.1f}%")
        print(f"  阶段1偏向获胜方向: {summary['盈利周期_阶段1_偏向获胜方向比例']*100:.1f}%")
        print(f"  阶段2偏向获胜方向: {summary['盈利周期_阶段2_偏向获胜方向比例']*100:.1f}%")
        print(f"  阶段3偏向获胜方向: {summary['盈利周期_阶段3_偏向获胜方向比例']*100:.1f}%")
        print(f"  最终偏向获胜方向: {summary['盈利周期_最终_偏向获胜方向比例']*100:.1f}%")
    
    if summary['亏损周期数'] > 0:
        print("\n亏损周期特征:")
        print(f"  阶段1持仓比例: {summary['亏损周期_阶段1_平均持仓比例']*100:.1f}%")
        print(f"  阶段2持仓比例: {summary['亏损周期_阶段2_平均持仓比例']*100:.1f}% (变化: {summary['亏损周期_阶段2_平均持仓比例变化']*100:+.1f}%)")
        print(f"  阶段3持仓比例: {summary['亏损周期_阶段3_平均持仓比例']*100:.1f}% (变化: {summary['亏损周期_阶段3_平均持仓比例变化']*100:+.1f}%)")
        print(f"  最终持仓比例: {summary['亏损周期_最终_平均持仓比例']*100:.1f}%")
    
    print("\n【策略模式总结】")
    print("\n1. 建仓阶段（0-5分钟）:")
    if summary['阶段1_平均持仓比例'] > 0.45 and summary['阶段1_平均持仓比例'] < 0.55:
        print("   - 策略倾向于双向平衡建仓，UP和DOWN持仓接近1:1")
    else:
        print(f"   - 策略在初期有轻微偏向，UP持仓比例约{summary['阶段1_平均持仓比例']*100:.1f}%")
    
    print("\n2. 调整阶段（5-10分钟）:")
    if summary['阶段2_平均持仓比例变化'] > 0.05:
        print("   - 策略在中期明显增加UP方向持仓")
    elif summary['阶段2_平均持仓比例变化'] < -0.05:
        print("   - 策略在中期明显增加DOWN方向持仓")
    else:
        print("   - 策略在中期保持相对平衡，小幅调整")
    
    print("\n3. 锁定阶段（10-15分钟）:")
    if summary['阶段3_平均持仓比例变化'] > 0.05:
        print("   - 策略在后期大幅增加UP方向持仓，锁定UP方向")
    elif summary['阶段3_平均持仓比例变化'] < -0.05:
        print("   - 策略在后期大幅增加DOWN方向持仓，锁定DOWN方向")
    else:
        print("   - 策略在后期继续小幅调整，未明显锁定方向")
    
    if summary['盈利周期数'] > 0:
        print("\n4. 盈利周期关键特征:")
        if summary['盈利周期_阶段3_偏向获胜方向比例'] > 0.7:
            print("   - 盈利周期中，70%以上在阶段3已偏向最终获胜方向")
        if summary['盈利周期_最终_偏向获胜方向比例'] > 0.8:
            print("   - 盈利周期中，80%以上最终持仓偏向获胜方向")
    
    print("\n" + "="*80)

if __name__ == '__main__':
    # 切换到脚本所在目录
    os.chdir(os.path.dirname(os.path.abspath(__file__)))
    
    # 分析所有周期
    print("开始分析所有周期...")
    df = analyze_all_cycles()
    
    # 保存详细分析数据
    df.to_csv('strategy_analysis_detail.csv', index=False, encoding='utf-8-sig')
    print(f"\n详细分析数据已保存到: strategy_analysis_detail.csv")
    
    # 生成策略总结
    summary = generate_strategy_summary(df)
    
    # 打印分析报告
    print_detailed_analysis(df, summary)
    
    # 保存总结
    summary_df = pd.DataFrame([summary])
    summary_df.to_csv('strategy_summary.csv', index=False, encoding='utf-8-sig')
    print(f"\n策略总结已保存到: strategy_summary.csv")
    
    # 打印每个周期的详细数据
    print("\n【各周期详细数据】")
    print(df[['周期开始时间', '最终利润', '胜负方向', '阶段1_持仓比例', '阶段2_持仓比例', 
              '阶段3_持仓比例', '最终_持仓比例', '阶段2_持仓比例变化', '阶段3_持仓比例变化']].to_string(index=False))

